# ADR 0002: Policy engine — Cedar primary, YAML front-end, stable evaluator interface

**Status:** Accepted · 2026-09-29

## Context

Tool-call policy needs (a) a human-writable language, (b) explainable decisions ("why was this denied?"), (c) an embeddable engine. Candidates: OPA/Rego, AWS Cedar, OpenFGA/Zanzibar-style, or a custom YAML scheme.

## Decision

1. **User-facing:** YAML policy files (`version`, ordered rules, `allow`/`deny`/`confirm`, glob tool matching, reasons).
2. **Engine:** Cedar (`cedar-policy/cedar-go`) as the decision engine — Cedar is purpose-built for authorization, has explainable diagnostics, and joined CNCF.
3. **Stability seam:** a small `policy.Evaluator` interface so engine choices (Rego, custom) remain swappable.

## Rationale

- **Explainable deny is our differentiator** (THREAT_MODEL claim #1 needs a reason string in the log). Cedar diagnostics + our explicit `reason` fields cover it.
- Rego is powerful but poor DX for this audience and use case; it remains a possible escape hatch behind the `Evaluator` interface.
- YAML-first keeps v0 shippable this week; Cedar compilation is internal detail.

## Consequences

- v0 evaluates YAML rules natively; Cedar wiring lands week 3 (ROADMAP) — the interface is designed so this is non-breaking.
- Policy files are part of the trust boundary: `policy test` exists to validate before deployment.
