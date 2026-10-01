// Package mediator implements tapelog's decision pipeline — the single
// place where a tool call is recorded, flow-checked, policy-evaluated,
// human-confirmed, and verdicted. Both `tapelog record` (transparent
// single-server proxy) and `tapelog mux` (multi-server aggregator) route
// through it so enforcement semantics are identical everywhere.
package mediator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/inject"
	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/limits"
	"github.com/Caseymccallum/tapelog/internal/plugin"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/schemafire"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/internal/taint"
)

// Options configures a Mediator.
type Options struct {
	SessionID      string
	Task           string
	Evaluator      policy.Evaluator
	Confirmer      approval.Confirmer
	Plugins        *plugin.Chain // optional verdict/redact plugins
	Limits         *limits.Tracker      // optional session budgets / caps
	Schemas        *schemafire.Validator // optional inbound schema firewall
	Injection      *inject.Scanner       // optional result injection scanning
	InjectionMode  string                // log | confirm | deny ("" = log)
	Values         *taint.Store          // optional value-level taint (CaMeL-style)
	NonInteractive bool          // wording for confirm denials
	DenyOnDrift    bool
	// AuditMode selects what happens when the session log cannot be
	// written (disk full, permissions, replaced by a directory, ...):
	//
	//	strict      (default) — nothing executes unrecorded: the affected
	//	            call is denied and every later call fails closed with
	//	            rule_id "audit-degraded". Availability is sacrificed
	//	            for audit integrity — the product's premise is
	//	            record + enforce + replay, so an unrecorded action is
	//	            an unauditable side effect.
	//	best-effort — explicit opt-out: warn on stderr and keep enforcing.
	//	            The session log may be missing events.
	AuditMode string
	Writer    EventWriter
	Redactor  *session.Redactor
	// Blobs + BlobThreshold enable large-payload offloading
	// (spec/session-log-v0.md §6.2): args/result over BlobThreshold bytes
	// are stored in Blobs and the event carries the digest reference.
	// Nil Blobs or threshold <= 0 keeps payloads inline (default).
	Blobs         *session.BlobStore
	BlobThreshold int
	// ResultWait bounds how long a decision waits for in-flight calls to
	// report results before value-level taint evaluation (0 = 2s default).
	// See awaitResults: an unresolved source degrades value flows to
	// conservative semantics, never to a silent pass.
	ResultWait time.Duration
}

// Audit modes for Options.AuditMode.
const (
	AuditStrict     = "strict"
	AuditBestEffort = "best-effort"
)

// EventWriter appends events to the session log. *session.Writer is the
// production implementation; tests inject failures to exercise the audit
// fail-closed state machine.
type EventWriter interface {
	Append(t session.EventType, payload any) (*session.Event, error)
}

// Outcome is the verdict for one tool call.
type Outcome struct {
	Allowed bool
	Verdict string // evaluated verdict (allow|confirm|deny)
	RuleID  string
	Reason  string
}

// ResultBlock says a recorded result must not be delivered to the
// harness as-is (payload cap exceeded, or injection markers blocked).
type ResultBlock struct {
	Code   string // machine-readable: response_too_large | result_blocked
	RuleID string
	Reason string
}

// Mediator runs the decision pipeline. Safe for concurrent use.
type Mediator struct {
	opts  Options
	taint policy.TaintState

	mu           sync.Mutex
	pins         map[string]pin      // tool -> descriptor pin
	pendingTools map[string]string   // JSON-RPC id -> tool (value taint)
	links        map[string]callLink // JSON-RPC id -> causation link
	auditErr     error               // sticky: first session-log append failure
}

type pin struct {
	hash  string
	drift bool
}

// callLink is the causation/correlation state for one in-flight request
// (spec/session-log-v0.md §6.1): which tools/call event spawned it and
// what trace context the transport carried.
type callLink struct {
	seq         uint64
	traceparent string
}

// New creates a Mediator.
func New(opts Options) *Mediator {
	if opts.Redactor == nil {
		opts.Redactor = session.NewRedactor()
	}
	if !opts.Plugins.Empty() {
		opts.Redactor.RegisterTextRedactor(func(s string) string {
			return opts.Plugins.RedactText(context.Background(), s)
		})
	}
	return &Mediator{opts: opts, pins: map[string]pin{}, pendingTools: map[string]string{}, links: map[string]callLink{}}
}

// HashDescriptor redacts + canonicalizes a tool descriptor and returns its
// pin hash (hex SHA-256) — consistent with what the session log stores.
func (m *Mediator) HashDescriptor(desc json.RawMessage) (string, json.RawMessage) {
	clean := m.opts.Redactor.RedactJSON(desc)
	return session.HashBytes(clean), clean
}

// PinDescriptor records a descriptor hash for a tool and reports whether it
// drifted since first seen (tool poisoning / rug-pull detection). Drift is
// sticky for the session.
func (m *Mediator) PinDescriptor(tool, hash string) (drift bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.pins[tool]; ok && old.hash != hash {
		m.pins[tool] = pin{hash: hash, drift: true}
		return true
	}
	m.pins[tool] = pin{hash: hash}
	return m.pins[tool].drift
}

// Descriptor returns the current pin for a tool.
func (m *Mediator) Descriptor(tool string) (hash string, drift bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pins[tool]
	return p.hash, p.drift
}

// PinSchema registers a tool's advertised inputSchema with the schema
// firewall (extracted from the raw descriptor). Broken schemas are
// reported and skipped (fail-open).
func (m *Mediator) PinSchema(tool string, descriptor json.RawMessage) error {
	if m.opts.Schemas == nil {
		return nil
	}
	var d struct {
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(descriptor, &d); err != nil {
		return err
	}
	return m.opts.Schemas.Set(tool, d.InputSchema)
}

// appendEvent records one session event and reports append failure.
// Failures latch: the first one switches the mediator to audit-degraded
// and (in strict mode) every later call is denied — see Options.AuditMode.
func (m *Mediator) appendEvent(t session.EventType, payload any) (*session.Event, error) {
	ev, err := m.opts.Writer.Append(t, payload)
	if err == nil {
		return ev, nil
	}
	m.mu.Lock()
	first := m.auditErr == nil
	if first {
		m.auditErr = err
	}
	m.mu.Unlock()
	if first {
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — session log append failed: %v (audit mode: %s)%s\n",
			err, m.auditMode(), " — recording is degraded for the rest of this session")
	}
	return nil, err
}

// auditMode returns the effective audit mode ("" = strict).
func (m *Mediator) auditMode() string {
	if m.opts.AuditMode == AuditBestEffort {
		return AuditBestEffort
	}
	return AuditStrict
}

// auditDegraded reports whether a session-log append has failed and, if
// so, the first failure.
func (m *Mediator) auditDegraded() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.auditErr != nil, m.auditErr
}

// RecordToolsList appends a tools/list event to the session log.
func (m *Mediator) RecordToolsList(tools []json.RawMessage) {
	cleaned := make([]json.RawMessage, 0, len(tools))
	for _, t := range tools {
		cleaned = append(cleaned, m.opts.Redactor.RedactJSON(t))
	}
	_, _ = m.appendEvent(session.EventToolsList, session.ToolsListPayload{Tools: cleaned})
}

// awaitResults blocks until every previously forwarded call has reported
// its result (or timeout elapses). Value-level taint is derived from
// RESULTS; without this gate a pipelined sink call could be evaluated
// before the source call's data was recorded (a causal race the e2e
// caught).
//
// It returns the tool names of calls still in flight at timeout (nil =
// fully drained). Callers MUST treat those as unresolved sources: value
// flows degrade to conservative semantics for them (assumed tainting),
// never to a silent pass — the precision model must not quietly become
// a timing-dependent model.
func (m *Mediator) awaitResults(timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.pendingTools)
		m.mu.Unlock()
		if n == 0 {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := make([]string, 0, len(m.pendingTools))
	for _, tool := range m.pendingTools {
		pending = append(pending, tool)
	}
	return pending
}

// Decide runs the full pipeline for one `tools/call` and records the
// events. id is the JSON-RPC request id (recorded verbatim).
func (m *Mediator) Decide(id json.RawMessage, tool string, args json.RawMessage) Outcome {
	return m.DecideMeta(id, tool, args, nil)
}

// DecideMeta is Decide with the request's `_meta` object (MCP metadata
// passthrough): `_meta.traceparent` is recorded on the call, decision,
// and result events (spec/session-log-v0.md §6.1). meta may be nil.
func (m *Mediator) DecideMeta(id json.RawMessage, tool string, args, meta json.RawMessage) Outcome {
	traceparent := jsonrpc.TraceparentOf(meta)
	// Audit integrity first: once the log is degraded, strict mode stops
	// forwarding — a tool call we cannot record must not execute.
	if degraded, cause := m.auditDegraded(); degraded && m.auditMode() == AuditStrict {
		reason := fmt.Sprintf("audit log is unwritable (%v); failing closed so nothing executes unrecorded (set audit mode best-effort to override)", cause)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "audit-degraded", Reason: reason}
	}
	descHash, drift := m.Descriptor(tool)
	redArgs := m.opts.Redactor.RedactJSON(args)
	if b, err := m.offload(redArgs); err != nil {
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — blob offload failed: %v (payload recorded inline)\n", err)
	} else {
		redArgs = b
	}
	callEv, err := m.appendEvent(session.EventToolCall, session.ToolCallPayload{
		ID: id, Tool: tool,
		Args:               redArgs,
		ToolDescriptorHash: descHash, DescriptorDrift: drift,
		Traceparent:        traceparent,
	})
	if err != nil && m.auditMode() == AuditStrict {
		reason := fmt.Sprintf("could not record the tool call (%v); failing closed (set audit mode best-effort to override)", err)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "audit-degraded", Reason: reason}
	}
	// Causation: every follow-on event for this request points back at
	// the tools/call event (spec/session-log-v0.md §6.1).
	var callSeq uint64
	if callEv != nil {
		callSeq = callEv.Seq
	}
	m.noteLink(id, callSeq, traceparent)

	// 1. Descriptor drift: possible tool poisoning (THREAT_MODEL #2).
	// Value-level taint needs prior results recorded before we judge
	// this call; wait briefly for in-flight calls to report. Sources
	// still unresolved at timeout are assumed to taint this call below
	// (conservative degradation — see awaitResults).
	var unresolved []string
	if m.opts.Values != nil {
		wait := m.opts.ResultWait
		if wait <= 0 {
			wait = 2 * time.Second
		}
		unresolved = m.awaitResults(wait)
	}
	if m.opts.DenyOnDrift && drift {
		reason := "tool descriptor changed since first listing (possible tool poisoning)"
		m.recordDecision(id, "deny", "descriptor-drift", reason)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "descriptor-drift", Reason: reason}
	}

	// 2. Schema firewall: arguments must satisfy the advertised inputSchema.
	if m.opts.Schemas != nil {
		if ok, reason := m.opts.Schemas.Check(tool, args); !ok {
			m.recordDecision(id, "deny", "schema-firewall", reason)
			return Outcome{Allowed: false, Verdict: "deny", RuleID: "schema-firewall", Reason: reason}
		}
	}

	// 3. Session limits: budgets, per-tool caps, rate limits.
	if m.opts.Limits != nil {
		if v := m.opts.Limits.Check(tool); v != nil {
			m.recordDecision(id, "deny", v.RuleID, v.Reason)
			return Outcome{Allowed: false, Verdict: "deny", RuleID: v.RuleID, Reason: v.Reason}
		}
	}

	// 4. Flow rules (toxic-flow guards) take precedence over per-call
	// rules. Value-mode rules (CaMeL-style, precise) are consulted first:
	// they fire only when the call's arguments carry contaminated values
	// from a source tool's result. Session-mode rules then apply their
	// conservative whole-session taint.
	var dec policy.Decision
	decided := false
	if vfc, ok := m.opts.Evaluator.(policy.ValueFlowChecker); ok && m.opts.Values != nil {
		contaminated := m.opts.Values.ContaminatedBy(args)
		if len(unresolved) > 0 {
			// Conservative degradation: a source call that has not
			// reported yet cannot prove its result is clean, so it is
			// assumed to taint this call. Precision yields to safety
			// under unresolved in-flight requests; the decision reason
			// names the assumption.
			contaminated = append(contaminated, unresolved...)
		}
		if fdec, applied := vfc.CheckFlowValues(&m.taint, tool, contaminated); applied {
			dec, decided = fdec, true
		}
	}
	if !decided {
		if fc, ok := m.opts.Evaluator.(policy.FlowChecker); ok {
			if fdec, applied := fc.CheckFlow(&m.taint, tool); applied {
				dec, decided = fdec, true
			}
		}
	}
	if !decided {
		// Policy sees the same (redacted) args the log records: what-if
		// re-evaluation runs on recorded args, so evaluating redacted here
		// keeps live verdicts reproducible from the log (the redaction is
		// deterministic). Value-taint matching (above) still sees raw args.
		dec = m.opts.Evaluator.Evaluate(policy.Request{
			SessionID: m.opts.SessionID, Task: m.opts.Task, Tool: tool,
			Args: m.opts.Redactor.RedactJSON(args),
		})
	}

	// 5. Plugin verdict hooks may TIGHTEN the decision (never loosen —
	// host-enforced); plugin failures fail closed.
	if !m.opts.Plugins.Empty() {
		t := m.opts.Plugins.Tighten(context.Background(), plugin.Request{
			Tool: tool, Args: args,
			Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: dec.Reason,
		})
		dec.Verdict, dec.Reason = policy.Verdict(t.Verdict), t.Reason
	}

	// 6. Human confirmation for `confirm` verdicts.
	finalVerdict, reason := dec.Verdict, dec.Reason
	if dec.Verdict == policy.VerdictConfirm {
		choice, extra := m.ask(tool, args)
		switch choice {
		case approval.ChoiceAllowOnce:
			finalVerdict = policy.VerdictAllow
			if extra != "" {
				reason += extra
			} else {
				reason += " (confirmed by human: allow once)"
			}
		case approval.ChoiceAllowSession:
			finalVerdict = policy.VerdictAllow
			if extra != "" {
				reason += extra
			} else {
				reason += " (confirmed by human: allow for session)"
			}
		default:
			finalVerdict = policy.VerdictDeny
			switch {
			case extra != "":
				reason += extra
			case m.opts.NonInteractive:
				reason += " (confirmation required; non-interactive session — use --auto-confirm or a terminal)"
			default:
				reason += " (denied by human)"
			}
		}
	}

	if finalVerdict != policy.VerdictAllow {
		_ = m.recordDecision(id, string(dec.Verdict), dec.RuleID, reason)
		return Outcome{Allowed: false, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
	}
	// The verdict is recorded BEFORE the call is allowed to proceed: an
	// allow we cannot record must not execute (strict mode).
	if err := m.recordDecision(id, string(dec.Verdict), dec.RuleID, reason); err != nil && m.auditMode() == AuditStrict {
		dreason := fmt.Sprintf("could not record the policy decision (%v); failing closed (set audit mode best-effort to override)", err)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "audit-degraded", Reason: dreason}
	}
	m.taint.Record(tool) // permitted: its data enters the agent's context
	m.noteCall(id, tool) // remember id -> tool for value taint at result time
	return Outcome{Allowed: true, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
}

// noteCall remembers which tool a forwarded call belongs to so Result can
// label its values with the right source (value-level taint).
func (m *Mediator) noteCall(id json.RawMessage, tool string) {
	if m.opts.Values == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pendingTools) > 4096 {
		m.pendingTools = map[string]string{} // pathological volume: drop oldest wholesale
	}
	m.pendingTools[string(id)] = tool
}

// noteLink remembers the causation link (parent tools/call seq + trace
// context) for a request id, so its decision and result events can point
// back at it (spec/session-log-v0.md §6.1).
func (m *Mediator) noteLink(id json.RawMessage, seq uint64, traceparent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.links) > 4096 {
		m.links = map[string]callLink{} // pathological volume: drop oldest wholesale
	}
	m.links[string(id)] = callLink{seq: seq, traceparent: traceparent}
}

// linkFor returns the causation link recorded for a request id.
func (m *Mediator) linkFor(id json.RawMessage) callLink {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.links[string(id)]
}

// dropLink forgets a finished request's causation link.
func (m *Mediator) dropLink(id json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, string(id))
}

// Result records a tool result for the call with the given id. A non-nil
// ResultBlock means the harness must not receive the payload as-is
// (over limits.max_response_bytes, or blocked by injection mode: deny):
// the (redacted) original is still recorded as evidence either way.
func (m *Mediator) Result(id json.RawMessage, isError bool, result json.RawMessage) *ResultBlock {
	link := m.linkFor(id)
	defer m.dropLink(id) // the request is finished: its link has been used
	redResult := m.opts.Redactor.RedactJSON(result)
	if b, err := m.offload(redResult); err != nil {
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — blob offload failed: %v (payload recorded inline)\n", err)
	} else {
		redResult = b
	}
	_, recErr := m.appendEvent(session.EventToolResult, session.ToolResultPayload{
		ID: id, IsError: isError, Result: redResult,
		ParentSeq: parentOf(link), Traceparent: link.traceparent,
	})
	if recErr != nil && m.auditMode() == AuditStrict {
		// The result cannot reach the harness unrecorded: replace it with
		// a structured error (the audit trail is the product).
		return &ResultBlock{
			Code:   "audit_degraded",
			RuleID: "audit-degraded",
			Reason: fmt.Sprintf("could not record the tool result (%v); delivery blocked (set audit mode best-effort to override)", recErr),
		}
	}
	// Label the result's values with their source tool (value-level taint).
	if m.opts.Values != nil {
		m.mu.Lock()
		tool := m.pendingTools[string(id)]
		delete(m.pendingTools, string(id))
		m.mu.Unlock()
		if tool != "" {
			m.opts.Values.Mark(tool, result)
		}
	}
	if m.opts.Limits != nil {
		if v := m.opts.Limits.CheckResponse(len(result)); v != nil {
			return &ResultBlock{Code: "response_too_large", RuleID: v.RuleID, Reason: v.Reason}
		}
	}
	if m.opts.Injection != nil {
		findings := m.opts.Injection.Scan(string(result))
		if len(findings) > 0 {
			reason := "result contains prompt-injection markers: " + findings[0].Pattern
			if n := len(findings); n > 1 {
				reason += fmt.Sprintf(" (+%d more)", n-1)
			}
			switch m.opts.InjectionMode {
			case "deny":
				m.recordDecision(id, "deny", "injection-scan", reason)
				return &ResultBlock{Code: "result_blocked", RuleID: "injection-scan", Reason: reason}
			case "confirm":
				args, _ := json.Marshal(map[string]any{
					"note": "deliver tool result containing injection markers?", "findings": findings,
				})
				choice, extra := m.ask("injection-review", args)
				if choice != approval.ChoiceAllowOnce && choice != approval.ChoiceAllowSession {
					reason += " (delivery blocked by human)"
					if extra != "" {
						reason += extra
					}
					m.recordDecision(id, "deny", "injection-scan", reason)
					return &ResultBlock{Code: "result_blocked", RuleID: "injection-scan", Reason: reason}
				}
				reason += " (delivered with human approval)"
				if extra != "" {
					reason += extra
				}
				m.recordDecision(id, "allow", "injection-scan", reason)
			default: // log
				m.recordDecision(id, "allow", "injection-scan", reason)
			}
		}
	}
	return nil
}

// ask consults the human for a confirm verdict, returning the choice plus
// an audit suffix when the confirmer explains itself (approval queue).
func (m *Mediator) ask(tool string, args json.RawMessage) (approval.Choice, string) {
	if ec, ok := m.opts.Confirmer.(approval.ExplainedConfirmer); ok {
		return ec.ConfirmExplain(tool, args)
	}
	return m.opts.Confirmer.Confirm(tool, args), ""
}

// MalformedCall records a boundary rejection of an unparseable
// tools/call. Nothing crosses the boundary unmediated: a call whose
// params cannot be parsed is denied at the boundary, recorded as
// evidence (tool name empty — none was extractable), and never
// forwarded. The harness receives JSON-RPC -32602 (the proxy/mux layer
// synthesizes it). Recording failures latch audit-degraded like any
// other event.
func (m *Mediator) MalformedCall(id, params json.RawMessage, cause error) {
	reason := fmt.Sprintf("malformed tools/call: %v (rejected at the boundary, never forwarded)", cause)
	_, _ = m.appendEvent(session.EventToolCall, session.ToolCallPayload{
		ID:   id,
		Tool: "",
		Args: m.opts.Redactor.RedactJSON(params),
	})
	_ = m.recordDecision(id, "deny", "malformed-request", reason)
}

func (m *Mediator) recordDecision(id json.RawMessage, verdict, ruleID, reason string) error {
	link := m.linkFor(id)
	_, err := m.appendEvent(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: id, Verdict: verdict, RuleID: ruleID, Reason: reason,
		ParentSeq: parentOf(link), Traceparent: link.traceparent,
	})
	return err
}

// parentOf renders a causation link's parent seq as the optional payload
// field (nil when the parent event is unknown).
func parentOf(link callLink) *uint64 {
	if link.seq == 0 {
		return nil
	}
	seq := link.seq
	return &seq
}

// offload stores an oversized payload in the blob store and returns its
// digest-reference placeholder (spec/session-log-v0.md §6.2). Payloads
// under the threshold — and everything when blob offload is disabled —
// pass through unchanged.
func (m *Mediator) offload(payload json.RawMessage) (json.RawMessage, error) {
	if m.opts.Blobs == nil || m.opts.BlobThreshold <= 0 {
		return payload, nil
	}
	return m.opts.Blobs.Offload(payload, m.opts.BlobThreshold)
}

// NamespacedName builds a mux tool name: <server>__<tool>.
func NamespacedName(server, tool string) string {
	return server + "__" + tool
}

// SplitNamespacedName splits a mux tool name into server and tool.
func SplitNamespacedName(name string) (server, tool string, ok bool) {
	server, tool, found := strings.Cut(name, "__")
	if !found || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// String renders an outcome for logs/tests.
func (o Outcome) String() string {
	return fmt.Sprintf("%s (%s): %s", o.Verdict, o.RuleID, o.Reason)
}
