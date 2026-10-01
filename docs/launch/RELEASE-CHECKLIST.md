# Release Checklist

## v0.4.0 (current — launch candidate `4f777cf`+)

- [x] Feature cut: MCP compat lab (hermetic matrix + real-world tier),
      signed checkpoints (SSHSIG/cosign + Rekor witness), DENIED provenance
      in `inspect`, trajectory assertion hardening (count ceilings, flow
      assertions, `max_depth`), schema causation/correlation + blob refs
      (spec §6.1/§6.2), external-audit fixes (P0 boundary rejections,
      replay verify-first, conservative value-taint), raw-protocol
      fail-closed invariant, `docs/QUICKSTART.md`
- [ ] **Release-day version transition** — every `0.4.0-dev` reference in
      the public onboarding path must become the release version:
      `internal/buildinfo/buildinfo.go` (source of truth; goreleaser
      stamps binaries from it at tag time), README status block,
      docs/QUICKSTART.md (`tapelog version` output line),
      docs/launch/TESTING-GUIDE.md (doctor output line), CONTRIBUTING.md,
      CHANGELOG.md status note. **Verify with
      `git grep -n "0\.4\.0-dev"` — expect zero hits outside historical CHANGELOG entries and this checklist after the bump.**
- [ ] CHANGELOG `v0.4` section cut + version set to `0.4.0`
- [ ] Demo GIF + human launch TODOs (domain, contacts, Show HN) — see
      `docs/ROADMAP.md` and `docs/launch/SHOW-HN.md` (post body now leads
      with the trajectory demo story)
- [ ] Walk the **five-minute test** on a clean machine
      (docs/QUICKSTART.md) and the second five minutes
      (examples/trajectory-demo) — it is the acceptance criterion
      (CONTRIBUTING.md)

## v0.3.x (folded into v0.4.0)

- [x] Feature cut: value-level taint (ADR 0005), fuzz operators v2,
      `tapelog test` CI integrations (`--junit`/`--annotate` + dogfood job),
      spec community docs, live-tamper-safe recording, live chain verdicts
      in both dashboards, `docs/CLIENT-SETUP.md`

## v0.2.0 (released)

- [x] Feature cut: `tapelog test`, `tapelog fuzz`, policy packs, multi-session
      dashboard, `tapelog doctor`, OTel decision spans
- [x] CHANGELOG `[0.2.0]` section cut; version set to `0.2.0`
- [x] All tests green (15 packages incl. packs harness), `go vet` clean
- [x] Annotated tag `v0.2.0` prepared locally
- [x] Release automation: `.github/workflows/release.yml` — goreleaser on
      tag push with per-archive SBOMs (syft) + keyless cosign signatures
      (SECURITY.md "Verifying releases" documents verification)
- [x] Version bumped to `0.3.0-dev` on main
- [x] Community readiness: bug/feature issue templates, PR template with
      the fixture/test gotchas, CONTRIBUTING "hard-won gotchas"
- [ ] **Human:** repo description + topics (`mcp`, `ai-agents`, `security`,
      `observability`, `replay`, `go`) + homepage in GitHub settings
- [ ] **Human:** `git push origin main` for these, then the next `v*` tag
      will exercise the signed release pipeline end-to-end
- [ ] **Human:** note v0.1.0's launch steps still stand (domain, demo GIF,
      contacts, Show HN) — v0.2.0 folds into the same launch

## v0.1.0

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

> First: walk through **[TESTING-GUIDE.md](TESTING-GUIDE.md)** (~30 min,
> copy-paste commands, all pre-validated) and tick its checklist.

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

## Post-release (v0.1.0 → v0.2.0 — done)

- [x] Bump `version` on main (now `0.3.0-dev`)
- [x] Goreleaser provenance + cosign signing + SBOM (SECURITY.md promise — ships with `release.yml`)
- [ ] Open the v0.2 candidates (docs/ROADMAP.md) as GitHub issues for
      community input
