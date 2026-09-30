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
	"strings"
	"sync"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/inject"
	"github.com/Caseymccallum/tapelog/internal/limits"
	"github.com/Caseymccallum/tapelog/internal/plugin"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/schemafire"
	"github.com/Caseymccallum/tapelog/internal/session"
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
	NonInteractive bool          // wording for confirm denials
	DenyOnDrift    bool
	Writer         *session.Writer
	Redactor       *session.Redactor
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

	mu   sync.Mutex
	pins map[string]pin // tool -> descriptor pin
}

type pin struct {
	hash  string
	drift bool
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
	return &Mediator{opts: opts, pins: map[string]pin{}}
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

// RecordToolsList appends a tools/list event to the session log.
func (m *Mediator) RecordToolsList(tools []json.RawMessage) {
	cleaned := make([]json.RawMessage, 0, len(tools))
	for _, t := range tools {
		cleaned = append(cleaned, m.opts.Redactor.RedactJSON(t))
	}
	_, _ = m.opts.Writer.Append(session.EventToolsList, session.ToolsListPayload{Tools: cleaned})
}

// Decide runs the full pipeline for one `tools/call` and records the
// events. id is the JSON-RPC request id (recorded verbatim).
func (m *Mediator) Decide(id json.RawMessage, tool string, args json.RawMessage) Outcome {
	descHash, drift := m.Descriptor(tool)
	_, _ = m.opts.Writer.Append(session.EventToolCall, session.ToolCallPayload{
		ID: id, Tool: tool,
		Args:               m.opts.Redactor.RedactJSON(args),
		ToolDescriptorHash: descHash, DescriptorDrift: drift,
	})

	// 1. Descriptor drift: possible tool poisoning (THREAT_MODEL #2).
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

	// 4. Flow rules (toxic-flow guards) take precedence over per-call rules.
	var dec policy.Decision
	decided := false
	if fc, ok := m.opts.Evaluator.(policy.FlowChecker); ok {
		if fdec, applied := fc.CheckFlow(&m.taint, tool); applied {
			dec, decided = fdec, true
		}
	}
	if !decided {
		dec = m.opts.Evaluator.Evaluate(policy.Request{
			SessionID: m.opts.SessionID, Task: m.opts.Task, Tool: tool, Args: args,
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
		switch m.opts.Confirmer.Confirm(tool, args) {
		case approval.ChoiceAllowOnce:
			finalVerdict = policy.VerdictAllow
			reason += " (confirmed by human: allow once)"
		case approval.ChoiceAllowSession:
			finalVerdict = policy.VerdictAllow
			reason += " (confirmed by human: allow for session)"
		default:
			finalVerdict = policy.VerdictDeny
			if m.opts.NonInteractive {
				reason += " (confirmation required; non-interactive session — use --auto-confirm or a terminal)"
			} else {
				reason += " (denied by human)"
			}
		}
	}

	m.recordDecision(id, string(dec.Verdict), dec.RuleID, reason)

	if finalVerdict != policy.VerdictAllow {
		return Outcome{Allowed: false, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
	}
	m.taint.Record(tool) // permitted: its data enters the agent's context
	return Outcome{Allowed: true, Verdict: string(dec.Verdict), RuleID: dec.RuleID, Reason: reason}
}

// Result records a tool result for the call with the given id. A non-nil
// ResultBlock means the harness must not receive the payload as-is
// (over limits.max_response_bytes, or blocked by injection mode: deny):
// the (redacted) original is still recorded as evidence either way.
func (m *Mediator) Result(id json.RawMessage, isError bool, result json.RawMessage) *ResultBlock {
	_, _ = m.opts.Writer.Append(session.EventToolResult, session.ToolResultPayload{
		ID: id, IsError: isError, Result: m.opts.Redactor.RedactJSON(result),
	})
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
				choice := m.opts.Confirmer.Confirm("injection-review", args)
				if choice != approval.ChoiceAllowOnce && choice != approval.ChoiceAllowSession {
					reason += " (delivery blocked by human)"
					m.recordDecision(id, "deny", "injection-scan", reason)
					return &ResultBlock{Code: "result_blocked", RuleID: "injection-scan", Reason: reason}
				}
				reason += " (delivered with human approval)"
				m.recordDecision(id, "allow", "injection-scan", reason)
			default: // log
				m.recordDecision(id, "allow", "injection-scan", reason)
			}
		}
	}
	return nil
}

func (m *Mediator) recordDecision(id json.RawMessage, verdict, ruleID, reason string) {
	_, _ = m.opts.Writer.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: id, Verdict: verdict, RuleID: ruleID, Reason: reason,
	})
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
