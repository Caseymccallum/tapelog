# ADR 0005: Value-level taint (CaMeL-inspired, experimental)

Date: 2026-09-30

## Context

Session-level flow rules (`flows:`) are conservative: once `read_secrets`
runs, ANY later sink call is restricted. Operators asked for precision —
block `send_http` when it *carries* secret data, allow it when it doesn't.
CaMeL (Google DeepMind, 2025) argues for language-level value tracking
through the model. We cannot see inside the model; we can see tool
results and tool arguments.

## Decision

Track taint at the value level with **contamination matching**:

1. Every forwarded call's RESULT is labeled with its source tool
   (`internal/taint.Store`, bounded: 512 values/source, 8 KiB/value,
   4-char minimum).
2. A sink call is checked for argument strings containing recorded source
   values (substring match). Flow rules gain `mode: session | value`.
3. Value-mode rules fire **only on contamination**; session-mode rules
   keep their conservative semantics. A results gate (`awaitResults`)
   closes the causal race between pipelined calls and result recording.

## Consequences

- Precision win: clean sinks pass after a source call (e2e-proven).
- **Heuristic, not proof**: base64/paraphrase/splitting evade matching;
  the model can transform values beyond recognition. Must-not-happen
  flows must keep `mode: session` rules. Documented in THREAT_MODEL.
- `tapelog fuzz` continues to evaluate session semantics (value mode is
  runtime-context dependent); documented limitation.
