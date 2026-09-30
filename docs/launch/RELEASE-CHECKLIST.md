# v0.1.0 Release Checklist

## ✅ Done (in-repo)

- [x] Feature complete: record / verify / replay / diff / inspect / export /
      policy (rules, Cedar `where`, grants, flows, limits, schema firewall,
      injection scanning, drift) / mux / plugins / sandbox / approval queue /
      web dashboard / completion
- [x] All tests green (14 packages), `go vet` clean, CI on push (Linux race + Windows)
- [x] Docs: README, ARCHITECTURE, THREAT_MODEL (9 claims + honest non-claims),
      POLICY reference, PLUGINS, spec/ (normative + conformance vectors), ADRs
- [x] Governance & trust: LICENSE (Apache-2.0), GOVERNANCE, CONTRIBUTING,
      CODE_OF_CONDUCT, SECURITY (private reporting), CHANGELOG (Keep a Changelog)
- [x] Release machinery: `.goreleaser.yaml` (static binaries, 3 OSes, version
      stamping), `.gitignore` hygiene
- [x] E2E demo: `examples/rogue-agent/demo.ps1` (offline attack story)
- [x] Naming audit (docs/launch/NAMING.md) — registries clear for `tapelog`
- [x] Version set to `0.1.0`; annotated tag `v0.1.0` prepared locally

## 👤 Human steps (before/at publish)

1. **Register `tapelog.dev`** (name audit says free — do this first)
2. **Record the demo GIF / short video** from `examples/rogue-agent/demo.ps1`
   (screen capture; embed in README + Show HN post)
3. **Fill contact placeholders** in SECURITY.md / CODE_OF_CONDUCT.md /
   docs/launch/SHOW-HN.md (security@ + personal email)
4. **Publish**: `git push origin main && git push origin v0.1.0` — the tag
   triggers goreleaser's release flow (verify the first run; signed releases
   + SBOM are the v0.1.1 follow-up per SECURITY.md)
5. **Post Show HN** (draft in docs/launch/SHOW-HN.md) at a good hour
   (US-morning Tue–Thu), pin the repo, add topics:
   `mcp`, `ai-agents`, `observability`, `security`, `replay`, `go`
6. Optional pre-publish: reserve `tapelog-mcp` on npm/PyPI for adapters

## Post-release

- [ ] Bump `version` to `0.2.0-dev` on main
- [ ] Goreleaser provenance + cosign signing + SBOM (SECURITY.md promise)
- [ ] Open the v0.2 candidates (docs/ROADMAP.md) as GitHub issues for
      community input
