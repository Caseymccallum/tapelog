# Contributing

Thanks for your interest! Cassette is in early construction — the highest-value contributions will come after the v0.1 scaffold lands, but issues labeled `help-wanted` and `good-first-issue` are fair game anytime.

## Development setup

Requirements: **Go ≥ 1.27**, `git`. (Windows/macOS/Linux all supported.)

```bash
git clone https://github.com/cassette-ai/cassette
cd cassette
go build ./...        # build everything
go test ./...         # run the test suite
go vet ./...          # static checks
```

No code generation, no container required, no services to run. If that's ever not true, this file will say so.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). In short: `cmd/cassette` (CLI), `internal/{jsonrpc,proxy,policy,session}`, `schema/` (the language-neutral log format), `docs/` (specs + ADRs).

## Pull requests

1. **Discuss substantial changes first** (GitHub issue/discussion) — per [GOVERNANCE.md](GOVERNANCE.md), schema/security-claim changes need an RFC.
2. Keep PRs focused; include tests for behavior changes.
3. `go test ./...` and `go vet ./...` must pass (CI runs them too).
4. Update docs alongside code — **documentation is part of the change, not a follow-up.** If you touch the log format, update `spec/session-log-v0.md` + `spec/schema/*.json` + the hash-chain tests.
5. Sign-off (`git commit -s`) is appreciated but not required pre-1.0.

## Coding conventions

- Standard `gofmt` / `go vet`; no heavy lint rule-churn early on.
- Errors are values: wrap with context (`fmt.Errorf("…: %w", err)`), never panic on user input.
- Determinism matters: anything touching the log format must be byte-reproducible (see `internal/session/canonical` tests).
- Comments explain *why*; the docs explain *what*.

## Testing expectations

- Format/security-relevant code (hash chain, redaction, policy decisions) requires table-driven tests **including negative cases** (tamper detection, pattern evasion).
- Tests must be hermetic: no network, no clock dependence (inject time), temp dirs via `t.TempDir()`.

## Community

Be kind and precise. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Questions welcome in GitHub Discussions — asking "why does X work this way?" often improves the docs.
