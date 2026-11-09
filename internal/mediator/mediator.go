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
	Plugins        *plugin.Chain         // optional verdict/redact plugins
	Limits         *limits.Tracker       // optional session budgets / caps
	Schemas        *schemafire.Validator // optional inbound schema firewall
	Injection      *inject.Scanner       // optional result injection scanning
	InjectionMode  string                // log | confirm | deny ("" = log)
	Values         *taint.Store          // optional value-level taint (CaMeL-style)
	NonInteractive bool                  // wording for confirm denials
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
	// OmitResultText replaces result bodies with a marker before redaction and
	// hashing (--omit-result-text). It exists for content that cannot be safely
	// redacted: prose, malformed or oversized payloads. The recording keeps the
	// shape of the result and none of its bytes.
	OmitResultText bool
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

	mu             sync.Mutex
	pins           map[string]pin         // tool -> descriptor pin
	pendingTools   map[string][]string    // JSON-RPC id -> tools awaiting a result (FIFO: ids may repeat)
	links          map[string][]*callLink // JSON-RPC id -> causation links (FIFO: ids may repeat)
	ambiguousIDs   map[string]bool        // ids seen with more than one call outstanding: pairing is unknowable
	ambiguousTools map[string][]string    // every tool that shared an ambiguous id (sticky per cluster)
	lostSources    []string               // sources whose pending entry was discarded by the cap: always assumed
	allSeenTools   []string               // distinct tool names seen, for conservative taint when a union overflows
	taintExhausted bool                   // a per-id union overflowed: fall back to all seen tools
	taintUnknown   bool                   // source capacity exhausted: some values have an unknowable source
	corrLost       bool                   // correlation state was discarded: no pairing can be attested after
	auditErr       error                  // sticky: first session-log append failure
	attrWarned     bool                   // one stderr warning per recording for rejected attribution
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
	// Client-declared attribution, carried from the request to the
	// decision and result events it causes (M11). Correlation only.
	attr jsonrpc.Attribution
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
	return &Mediator{opts: opts, pins: map[string]pin{}, pendingTools: map[string][]string{}, links: map[string][]*callLink{}, ambiguousIDs: map[string]bool{}, ambiguousTools: map[string][]string{}}
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
	for _, tools := range m.pendingTools {
		pending = append(pending, tools...)
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
func (m *Mediator) DecideMeta(id json.RawMessage, tool string, args, meta json.RawMessage) (out Outcome) {
	traceparent := jsonrpc.TraceparentOf(meta)
	attr, dropped := jsonrpc.AttributionOf(meta)
	if dropped > 0 {
		m.warnRejectedAttribution()
	}
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
	link := m.reserveLink(id, traceparent, attr)
	// A denied call never receives a result. Removing its own link is what
	// keeps the NEXT call under the same id from looking ambiguous (a phantom
	// still-outstanding call) and stops its attribution leaking onto an
	// unrelated result. Applies to every deny path below, not just policy.
	defer func() {
		if !out.Allowed {
			m.dropLinkRef(id, link)
		}
	}()
	callEv, err := m.appendEvent(session.EventToolCall, session.ToolCallPayload{
		ID: id, Tool: tool,
		Args:               redArgs,
		ToolDescriptorHash: descHash, DescriptorDrift: drift,
		Traceparent:    traceparent,
		ConversationID: attr.ConversationID,
		ToolCallID:     attr.ToolCallID,
		SubagentID:     attr.SubagentID,
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
	m.setLinkSeq(link, callSeq)

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
		m.recordDecision(*link, id, "deny", "descriptor-drift", reason)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "descriptor-drift", Reason: reason}
	}

	// 2. Schema firewall: arguments must satisfy the advertised inputSchema.
	if m.opts.Schemas != nil {
		if ok, reason := m.opts.Schemas.Check(tool, args); !ok {
			m.recordDecision(*link, id, "deny", "schema-firewall", reason)
			return Outcome{Allowed: false, Verdict: "deny", RuleID: "schema-firewall", Reason: reason}
		}
	}

	// 3. Session limits: budgets, per-tool caps, rate limits.
	if m.opts.Limits != nil {
		if v := m.opts.Limits.Check(tool); v != nil {
			m.recordDecision(*link, id, "deny", v.RuleID, v.Reason)
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

	// 5. Plugin verdict hooks may TIGHTEN the decision, never loosen it. The
	// clamp is enforced inside plugin.Chain.Tighten (TightenAllowed: only a
	// strictly stricter verdict is adopted, a loosening attempt is ignored and
	// logged), so the verdict copied out below cannot be weaker than dec's.
	// Plugin failures fail closed (deny).
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
		_ = m.recordDecision(*link, id, string(dec.Verdict), dec.RuleID, reason)
		return Outcome{Allowed: false, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
	}
	// The verdict is recorded BEFORE the call is allowed to proceed: an
	// allow we cannot record must not execute (strict mode).
	if err := m.recordDecision(*link, id, string(dec.Verdict), dec.RuleID, reason); err != nil && m.auditMode() == AuditStrict {
		dreason := fmt.Sprintf("could not record the policy decision (%v); failing closed (set audit mode best-effort to override)", err)
		return Outcome{Allowed: false, Verdict: "deny", RuleID: "audit-degraded", Reason: dreason}
	}
	m.taint.Record(tool) // permitted: its data enters the agent's context
	m.noteCall(id, tool) // remember id -> tool for value taint at result time
	return Outcome{Allowed: true, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
}

// maxAmbiguousTools bounds the per-id union of tools that shared an ambiguous
// request id (the union can only over-taint, so a truncated set stays safe).
const maxAmbiguousTools = 32

// maxSeenTools bounds the conservative fallback set. It is deliberately far
// larger than any real MCP catalog: past it, a late result could lose a source.
const maxSeenTools = 4096

func containsTool(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

// noteCall remembers which tool a forwarded call belongs to so Result can
// label its values with the right source (value-level taint). The queue is
// FIFO per request id: a result answers the oldest call still waiting under
// that id, and when more than one is waiting the id is ambiguous and the
// taint is applied conservatively (see Result).
func (m *Mediator) noteCall(id json.RawMessage, tool string) {
	if m.opts.Values == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pendingTools) > 4096 {
		// Discarding pending calls would otherwise UN-taint their results
		// (a source that vanishes can never be matched again). Keep them as
		// assumed sources instead: over-taint, never under-taint.
		for _, tools := range m.pendingTools {
			for _, tl := range tools {
				if tl != "" && !containsTool(m.lostSources, tl) && len(m.lostSources) < maxAmbiguousTools {
					m.lostSources = append(m.lostSources, tl)
				}
			}
		}
		m.pendingTools = map[string][]string{} // pathological volume: drop oldest wholesale
		m.ambiguousTools = map[string][]string{}
		m.corrLost = true
	}
	key := string(id)
	if tool != "" && !containsTool(m.allSeenTools, tool) {
		if len(m.allSeenTools) < maxSeenTools {
			m.allSeenTools = append(m.allSeenTools, tool)
		} else {
			// Capacity exhausted: this tool can never be named again, so any
			// value it produces has an unknowable source from here on. Fail
			// closed instead of quietly forgetting it.
			m.taintUnknown = true
		}
	}
	if len(m.pendingTools[key]) > 0 {
		// Sticky: every result under a shared id must be tainted by all the
		// tools that shared it, or the response belonging to the first call
		// could go untainted once the queue drains. Deduplicated and capped
		// so a client that reuses one id thousands of times cannot grow this
		// quadratically.
		for _, t := range append(append([]string(nil), m.pendingTools[key]...), tool) {
			if t == "" || containsTool(m.ambiguousTools[key], t) {
				continue
			}
			if len(m.ambiguousTools[key]) >= maxAmbiguousTools {
				// Dropping a source here would UNDER-taint: a policy rule
				// keyed on it would stop firing. Over-taint instead, using
				// every tool name this session has seen.
				m.taintExhausted = true
				continue
			}
			m.ambiguousTools[key] = append(m.ambiguousTools[key], t)
		}
	}
	m.pendingTools[key] = append(m.pendingTools[key], tool)
}

// noteLink remembers the causation link (parent tools/call seq + trace
// context) for a request id, so its decision and result events can point
// back at it (spec/session-log-v0.md §6.1).
// reserveLink registers a call's correlation BEFORE its events are appended.
// A response can arrive concurrently, and a link added afterwards would let
// that response observe a short queue and be declared unambiguous when it is
// not. The returned pointer stays valid even if the entry is consumed.
func (m *Mediator) reserveLink(id json.RawMessage, traceparent string, attr jsonrpc.Attribution) *callLink {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.links) > 4096 {
		m.links = map[string][]*callLink{} // pathological volume: drop oldest wholesale
		m.ambiguousIDs = map[string]bool{}
		// After discarding correlation state a delayed old response could be
		// paired with a NEW call under the same id and look exact. It is not.
		m.corrLost = true
	}
	// FIFO per request id. A JSON-RPC id may be reused once its call has
	// completed (a client that restarts its counter, or a long-lived
	// gateway), and a last-write-wins map would then give the later call's
	// attribution to the earlier one's result.
	key := string(id)
	if len(m.links[key]) > 0 {
		// Sticky for the rest of the session: once two calls share an id
		// while both are outstanding, no later response can be paired with
		// one of them from the transport alone.
		m.ambiguousIDs[key] = true
	}
	l := &callLink{traceparent: traceparent, attr: attr}
	m.links[key] = append(m.links[key], l)
	return l
}

// setLinkSeq fills in the call event's seq once it has been appended.
func (m *Mediator) setLinkSeq(l *callLink, seq uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l != nil {
		l.seq = seq
	}
}

// warnRejectedAttribution reports (once per recording) that a request
// carried attribution that failed validation. The value is never
// recorded — a malformed or oversized identifier is a different id, and
// recording it would be a false claim. stderr only: stdout is the
// JSON-RPC channel.
func (m *Mediator) warnRejectedAttribution() {
	m.mu.Lock()
	warned := m.attrWarned
	m.attrWarned = true
	m.mu.Unlock()
	if warned {
		return
	}
	fmt.Fprintf(os.Stderr, "tapelog: WARNING — ignoring conversation attribution in `_meta`: not a bounded identifier (max %d bytes, [A-Za-z0-9._:-]); it is client-declared correlation and was not recorded\n", jsonrpc.MaxAttributionLen)
}

// linkFor returns the causation link recorded for a request id (the oldest
// still-open one when the id has been reused).
func (m *Mediator) linkFor(id json.RawMessage) callLink {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q := m.links[string(id)]; len(q) > 0 {
		return *q[0]
	}
	return callLink{}
}

// consumeLink removes and returns the oldest causation link for a request
// id, and reports whether the id was ambiguous — more than one call still
// outstanding under it, so the response cannot be tied to one of them.
func (m *Mediator) consumeLink(id json.RawMessage) (callLink, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(id)
	q := m.links[key]
	ambiguous := m.ambiguousIDs[key] || m.corrLost
	if len(q) == 0 {
		// A result with no recorded call is never attributable.
		delete(m.ambiguousIDs, key)
		return callLink{}, true
	}
	if len(q) > 1 {
		m.links[key] = q[1:]
		return *q[0], true
	}
	// Last answer of this cluster: ambiguous if the cluster ever was, then the
	// cluster is complete and the id is clean again — a LATER sequential reuse
	// is genuinely unambiguous and must not be flagged.
	delete(m.links, key)
	delete(m.ambiguousIDs, key)
	return *q[0], ambiguous
}

// dropLinkRef removes one specific call's link, leaving any other calls that
// share the id untouched.
func (m *Mediator) dropLinkRef(id json.RawMessage, l *callLink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(id)
	q := m.links[key]
	for i, p := range q {
		if p == l {
			q = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(q) == 0 {
		delete(m.links, key)
		delete(m.ambiguousIDs, key)
		return
	}
	m.links[key] = q
}

// dropLink forgets a finished request's causation link.
func (m *Mediator) dropLink(id json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.links[string(id)]
	if len(q) <= 1 {
		delete(m.links, string(id))
		return
	}
	m.links[string(id)] = q[1:]
}

// Result records a tool result for the call with the given id. A non-nil
// ResultBlock means the harness must not receive the payload as-is
// (over limits.max_response_bytes, or blocked by injection mode: deny):
// the (redacted) original is still recorded as evidence either way.
func (m *Mediator) Result(id json.RawMessage, isError bool, result json.RawMessage) *ResultBlock {
	link, ambiguous := m.consumeLink(id)
	// original is what the harness receives and what the scanners below must see.
	// result becomes the recorded form: redacted, and in omit mode blanked. Scanning
	// the recorded form would let omission hide an injection marker or a size breach.
	original := result
	if m.opts.OmitResultText {
		// Before redaction and hashing: the omitted body is the recording, so replay
		// serves exactly what was kept rather than a redacted copy of something the
		// user asked not to store at all.
		result = session.OmitResultText(result)
	}
	redResult := m.opts.Redactor.RedactJSON(result)
	if b, err := m.offload(redResult); err != nil {
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — blob offload failed: %v (payload recorded inline)\n", err)
	} else {
		redResult = b
	}
	res := session.ToolResultPayload{ID: id, IsError: isError, Result: redResult}
	if ambiguous {
		// The id was shared by more than one outstanding call, so this
		// result cannot be tied to either. Record that it arrived and that
		// its correlation is unknown rather than picking one and presenting
		// a guess as a fact.
		res.AttributionAmbiguous = true
	} else {
		res.ParentSeq = parentOf(link)
		res.Traceparent = link.traceparent
		res.ConversationID = link.attr.ConversationID
		res.ToolCallID = link.attr.ToolCallID
		res.SubagentID = link.attr.SubagentID
	}
	_, recErr := m.appendEvent(session.EventToolResult, res)
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
	// When more than one call is outstanding under this id the response cannot
	// be tied to one of them. Mark it with the union of the candidates — that
	// can only over-taint, never miss a real source — and consume one in order
	// so the remaining results still get labelled.
	if m.opts.Values != nil {
		m.mu.Lock()
		key := string(id)
		q := m.pendingTools[key]
		union := append([]string(nil), m.ambiguousTools[key]...)
		lost := append([]string(nil), m.lostSources...)
		unknown := m.taintUnknown
		if m.taintExhausted {
			lost = append(lost, m.allSeenTools...)
		}
		if len(q) > 1 {
			m.pendingTools[key] = q[1:]
		} else {
			// Cluster complete: later sequential reuse of this id is exact
			// again, so the sticky union must not outlive the cluster.
			delete(m.pendingTools, key)
			delete(m.ambiguousTools, key)
		}
		m.mu.Unlock()
		seen := map[string]bool{}
		for _, tool := range append(append(q, union...), lost...) {
			if tool == "" || seen[tool] {
				continue
			}
			seen[tool] = true
			m.opts.Values.Mark(tool, original)
		}
		if unknown {
			// Explicit unknowable source, recorded as a session fact so a
			// value-bucket eviction cannot turn it back into "clean".
			m.opts.Values.MarkUnknown()
		}
	}
	if m.opts.Limits != nil {
		if v := m.opts.Limits.CheckResponse(len(original)); v != nil {
			return &ResultBlock{Code: "response_too_large", RuleID: v.RuleID, Reason: v.Reason}
		}
	}
	if m.opts.Injection != nil {
		findings := m.opts.Injection.Scan(string(original))
		if len(findings) > 0 {
			reason := "result contains prompt-injection markers: " + findings[0].Pattern
			if n := len(findings); n > 1 {
				reason += fmt.Sprintf(" (+%d more)", n-1)
			}
			switch m.opts.InjectionMode {
			case "deny":
				m.recordDecision(link, id, "deny", "injection-scan", reason)
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
					m.recordDecision(link, id, "deny", "injection-scan", reason)
					return &ResultBlock{Code: "result_blocked", RuleID: "injection-scan", Reason: reason}
				}
				reason += " (delivered with human approval)"
				if extra != "" {
					reason += extra
				}
				m.recordDecision(link, id, "allow", "injection-scan", reason)
			default: // log
				m.recordDecision(link, id, "allow", "injection-scan", reason)
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
// tools/call (or an unparseable/undispatchable line). Nothing crosses
// the boundary unmediated: a request the boundary cannot parse is
// denied at the boundary, recorded as evidence (tool name empty — none
// was extractable, redacted raw bytes as args), and never forwarded.
// The harness receives JSON-RPC -32602/-32700 (the proxy/mux layer
// synthesizes it; notifications are refused silently). Recording
// failures latch audit-degraded like any other event.
func (m *Mediator) MalformedCall(id, params json.RawMessage, cause error) {
	reason := fmt.Sprintf("malformed tools/call: %v (rejected at the boundary, never forwarded)", cause)
	meta := jsonrpc.MetaOf(params) // best-effort: nil for unparseable params
	tp := jsonrpc.TraceparentOf(meta)
	attr, dropped := jsonrpc.AttributionOf(meta)
	if dropped > 0 {
		m.warnRejectedAttribution()
	}
	link := m.reserveLink(id, tp, attr)
	callEv, _ := m.appendEvent(session.EventToolCall, session.ToolCallPayload{
		ID:             id,
		Tool:           "",
		Args:           m.opts.Redactor.RedactJSON(params),
		Traceparent:    tp,
		ConversationID: attr.ConversationID,
		ToolCallID:     attr.ToolCallID,
		SubagentID:     attr.SubagentID,
	})
	// Causation: the rejection decision points back at its tools/call
	// event exactly like an ordinary mediated call (spec §6.1).
	var callSeq uint64
	if callEv != nil {
		callSeq = callEv.Seq
	}
	m.setLinkSeq(link, callSeq)
	_ = m.recordDecision(*link, id, "deny", "malformed-request", reason)
	m.dropLink(id) // no result will follow a rejection
}

func (m *Mediator) recordDecision(link callLink, id json.RawMessage, verdict, ruleID, reason string) error {
	_, err := m.appendEvent(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: id, Verdict: verdict, RuleID: ruleID, Reason: reason,
		ParentSeq: parentOf(link), Traceparent: link.traceparent,
		ConversationID: link.attr.ConversationID,
		ToolCallID:     link.attr.ToolCallID,
		SubagentID:     link.attr.SubagentID,
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
