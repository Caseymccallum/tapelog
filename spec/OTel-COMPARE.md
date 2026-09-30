# Session Log vs OpenTelemetry — honest comparison

OpenTelemetry is the obvious question: *"we already have traces — why a
new format?"* Short answer: **OTel is an observability pipeline; this is
an evidence + regression format.** They overlap on purpose (we export to
OTel), but they disagree on defaults.

## What OTel gives you

The [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
and OTLP spans cover a lot of the same raw material: model/tool spans,
`gen_ai.tool.name`, token counts, events, trace/span IDs, context
propagation. If your question is *"what was latency/token spend per
step?"* or *"where did this request spend its time?"* — OTel is the
right tool, and you should keep it.

## Where the session log differs

| Concern | OTel (traces) | Session log (JSONL) |
|---|---|---|
| Primary audience | dashboards, alerting | auditors, CI, replay engines |
| Transport shape | OTLP exporter → collector/backend | one file, zero infrastructure |
| **Tamper evidence** | not a goal (backends can rewrite) | hash chain per session (`verify`) |
| **Argument/result fidelity** | conventions truncate / attribute-level | full JSON args + results, verbatim |
| **Policy decisions** | spans if you emit them | first-class events (allow/deny/confirm, rule, reason) |
| **Deterministic replay** | not a goal | normative §8: same input → same events |
| Stability | semconv is `development` | canonical form pinned by vectors |

## Design choices, spelled out

1. **A file, not a pipeline.** The unit of evidence is one session. A
   JSONL file survives vendor changes, mails well as an attachment, and
   diffs in review. OTel's value appears at fleet scale; the session
   log's value appears at *one incident, one audit, one CI run*.
2. **Tamper evidence is the point.** Observability data flows through
   systems that sample, drop, and enrich. That's fine for metrics and
   fatal for evidence. The hash chain makes edits detectable without
   trusting the storage.
3. **Verbatim beats curated.** Semantic conventions attribute data for
   humans; replay needs the exact bytes. `args`/`result` are recorded
   as-was (after redaction).
4. **Decisions live next to actions.** An agent's risk surface is the
   tool call. Recording `tools/call` without its `verdict` is like
   logging HTTP without auth outcomes.

## Bridging (both directions)

- **tapelog → OTel**: tapelog exports decision spans (`internal/otelx`;
  `record --otel`) — each tool call as a span with verdict, rule, and
  session context, so you keep your dashboards. The session log remains
  the source of truth; spans are a projection.
- **OTel → session log**: a Reader conformance class implementation can
  be written against GenAI spans. Expect loss: sampling, attribute
  truncation, and missing verdicts mean reconstruction is best-effort.
  Where exactness matters, record natively.

## FAQ-pair with OTel

**"Can the session log just be a span exporter?"** It could, but the
canonical form + hash chain require byte-level control that OTLP's data
model doesn't guarantee.

**"Does this compete with OpenTelemetry Weaver / semconv tooling?"** No —
this format is small enough to read in an hour and pinned by vectors;
semconv solves cross-vendor telemetry naming at fleet scale.

**"Will you support OTLP ingest/export?"** Export exists (spans). OTLP
ingest is a community contribution (see FAQ).
