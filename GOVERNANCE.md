# Governance

## Principles

Cassette is founder-led in the pre-1.0 phase with a light, transparent process designed to grow into community governance. Decisions are made in public (GitHub issues/discussions). "Transparency is a feature" applies to the project itself.

## Decision making

1. **Day-to-day:** Maintainers (see MAINTAINERS.md, to be added) merge PRs using lazy consensus — review required, no objection within 72h ⇒ merge.
2. **Substantial changes:** An RFC-style GitHub discussion is required before implementation. Substantial = session-log schema changes, policy-language semantics, new enforcement surfaces, anything altering security claims in docs/THREAT_MODEL.md.
3. **Schema compatibility:** Changes to the session-log hash-chain canonical form are **breaking** and require a schema version bump (`v`), a migration note, and two maintainer approvals.
4. **Security-sensitive changes** follow SECURITY.md instead — private disclosure first, public RFC after fix.

## Contributor ladder

| Stage | Requirements | Rights |
|---|---|---|
| Contributor | Any merged PR | — |
| Reviewer | Consistent quality reviews | PR triage |
| Committer | Sustained contributions + domain knowledge | Merge rights |
| Maintainer | Stewardship of an area + community trust | Release authority, RFC approval |

## Roadmap & scope control

Scope is defined by docs/THREAT_MODEL.md in-scope table + docs/ROADMAP.md. Feature requests that expand the *security claims* require RFC; requests that expand *developer ergonomics* are prioritized in public roadmap issues.

## Conflicts of interest

If a change primarily benefits a maintainer's employer, the maintainer declares it in the PR and recuses from the final merge decision when practical.

## License & IP

Apache-2.0. All contributions are under the project license (inbound = outbound). No CLA pre-1.0; revisit if the project joins a foundation (CNCF neutral-governance is the aspirational home for the schema spec).
