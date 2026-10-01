# Contributing

Thanks for your interest! Tapelog is pre-1.0 and moving fast (see the
README status block for the current version) — issues labeled
`help-wanted` and `good-first-issue` are fair game anytime.

## The five-minute test (acceptance criterion)

A stranger must be able to understand tapelog, install it, put it in
front of one MCP server, record a real interaction, and replay it —
within five minutes ([docs/QUICKSTART.md](docs/QUICKSTART.md)). The
follow-on five minutes (record → denied call → inspect → verify →
replay → what-if) must stay demonstrable end to end
([examples/trajectory-demo](../examples/trajectory-demo)).

Every change is judged against that. If a change makes the product
slower to understand, install, or prove — or breaks the demo path — it
has to earn that cost explicitly in review. This is the product filter:
adoption friction is a bug.

## Development setup

Requirements: **Go ≥ 1.27**, `git`. (Windows/macOS/Linux all supported.)

```bash
git clone https://github.com/Caseymccallum/tapelog
cd tapelog
go build ./...        # build everything
go test ./...         # run the test suite
go vet ./...          # static checks
```

No code generation, no container required, no services to run. If that's ever not true, this file will say so.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). In short: `cmd/tapelog` (CLI), `internal/{jsonrpc,proxy,mediator,policy,session,replay,web,…}`, `spec/` (the language-neutral log format), `docs/` (guides + ADRs).

## Pull requests

1. **Discuss substantial changes first** (GitHub issue/discussion) — per [GOVERNANCE.md](GOVERNANCE.md), schema/security-claim changes need an RFC.
2. Keep PRs focused; include tests for behavior changes.
3. `go test ./...` and `go vet ./...` must pass (CI runs them too).
4. Update docs alongside code — **documentation is part of the change, not a follow-up.** If you touch the log format, update `spec/session-log-v0.md` + `spec/schema/*.json` + the hash-chain tests.
5. Sign-off (`git commit -s`) is appreciated but not required pre-1.0.


## Hard-won gotchas (please don't re-learn these)

1. **Simulate CI before pushing.** CI sees *tracked files only* — your
   working tree may contain ignored files that mask a failure:

   ```bash
   git clone . /tmp/tapelog-ci-sim && (cd /tmp/tapelog-ci-sim && go test ./...)
   ```

2. **Test fixtures vs `*.jsonl`.** `.gitignore` ignores `*.jsonl` ("session
   logs are user data"). Any *fixture* `.jsonl` needs an explicit
   `!path/to/fixture.jsonl` exception — otherwise it silently exists only
   on your machine and CI fails mysteriously. Check with
   `git status --ignored`.

3. **Tests must be slow-machine safe.** CI runners are 2-core shared VMs.
   Never bound a wait by iteration count; use `time.Now().Add(15*time.Second)`
   deadlines. If a test needs a server response, make the fake server reply
   *after* the request arrives (causality), never pre-loaded.

4. **Rebuild `bin/tapelog.exe` before e2e demos.** The binary is not part
   of `go build ./...` output. (Yes, this bit us three times.)

## Coding conventions

- Standard `gofmt` / `go vet`; no heavy lint rule-churn early on.
- Errors are values: wrap with context (`fmt.Errorf("…: %w", err)`), never panic on user input.
- Determinism matters: anything touching the log format must be byte-reproducible (see the canonical-form tests in `internal/session`).
- Comments explain *why*; the docs explain *what*.

## Testing expectations

- Format/security-relevant code (hash chain, redaction, policy decisions) requires table-driven tests **including negative cases** (tamper detection, pattern evasion).
- Tests must be hermetic: no network, no clock dependence (inject time), temp dirs via `t.TempDir()`.

## Community

Be kind and precise. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Questions welcome in GitHub Discussions — asking "why does X work this way?" often improves the docs.
