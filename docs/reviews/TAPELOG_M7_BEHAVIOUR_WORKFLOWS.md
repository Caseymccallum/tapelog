# Tapelog M7 — Behaviour CLI and Workflows

## Identity

Baseline: `feat/m6-agent-behaviour-intelligence` at `198f5b71ce9bc020060621905cc2f8436bbeba51`. Product implementation and tests: `c1b012615e32d23a5a0e12d82c96d06d8f7a72f0`; this report follows in a documentation commit. Work was done in an isolated clone and remains on `feat/m7-behaviour-cli-and-workflows`. No push, merge, release, live MCP call or external service was used.

## CLI and developer workflow

`tapelog behaviour summary`, `repeats`, and `failures` take `--dir` plus optional explicitly selected plain `.jsonl` filenames. `compare` takes `--dir`, `--reference` and `--candidate`. All four offer `--json`. They use the workbench's recording reader and M6 aggregation/comparison logic, with no HTTP request or server startup. See [the workflow guide](../BEHAVIOUR_CLI.md) and [fictional assertion](../../examples/behaviour-workflow/error-absence.yaml).

A fictional example, after creating a new demo directory:

```text
Behaviour failures · complete
Sessions 1 · calls 6 · results 6 · tool errors 4 · denied calls 0 · deny decisions 0 · unanswered 0
failures (1)
  db__query · id b4cf4281cee893b0 · occurrences 4 · sessions 1
```

The comparison includes new/resolved failure and denial groups, new/resolved/changed repeat patterns, M6 confidence labels, tool usage, rates with denominators and a bounded call comparison with unmatched occurrences and repeated-key ambiguity. A change is an observation, not an automatic regression. It emits a suggested default-loopback workbench link, but does not open it or start a listener. The dashboard must be started separately with the same directory.

Versioned JSON uses `schema_version: 1`, `analysis`, `status`, `argument_result_text_included: false`, and `result`. A fresh run sorts object groups deterministically while preserving chronological trends and ordered scalar sequences. Nullable values remain null. Ratio denominators are reported; for calls per minute, the call count is the numerator and explicit recorded duration in milliseconds is the denominator. Results are on stdout; diagnostics are on stderr. Verified process exits: 0 complete analysis, 2 partial/no evidence, 1 invalid command/input. `complete` means coverage and chain/end checks, not that every call received a result.

No raw argument/result text, error message labels, literal fragments, error codes, policy reasons, task names or harness values are emitted. Tool names, policy rule IDs, server names, file names, timestamps, event IDs and argument/result digests remain potentially sensitive metadata; low-entropy hashes can be guessed. These fields are not an anonymisation guarantee. The existing M6 failure normalizer is not a redaction system.

## Architecture and integrity

`internal/behaviour` now contains the shared M6 repeat detector and failure-message normalisation/fragment primitives. M6 web code calls these same functions; the CLI reuses the existing workbench analysis adapter, per-call association, aggregation and comparison instead of duplicating them. This extraction is **incremental**: recording discovery, aggregation and the CLI-facing workflow adapter still reside in `internal/web`, so the CLI still imports that package even though it invokes no HTTP handler. Moving those remaining responsibilities into a transport-neutral package is outstanding work and was not represented as complete.

M6's cache previously reused an analysis based only on file size and mtime. Each lookup now rereads the bounded file, verifies the chain and compares its content revision before reusing the index. A same-size/same-mtime changed-file test confirms the stale verified result is not returned. Cache hits still avoid repeated detection but no longer avoid the verified read; byte budgets apply to those reads. The recording writer, replay, policy enforcement and assertion semantics were not changed. The demo generator now requires a new directory and refuses an existing one, preventing accidental overwrite of recordings.

The comparison adds duration fields to the M6 rate representation, distinguishes repeat confidence, and corrects the per-comparison denied group's session count from zero to one. It augments the existing M6 behaviour delta with tool usage and call-level evidence. Existing UI consumers ignore the additive fields. CLI scanning is fresh for each invocation; the workbench cache uses the verified content revision.

## Assertion evidence and proposal

`result_never_contains` remains a literal, case-sensitive substring check over matching **recorded result JSON**, including MCP error results and multiple calls. Tests establish that no matching calls, unanswered calls and redacted results can pass without proving the behavior absent, and a changed signature or case can evade a literal check. `tapelog test` does not establish session completeness or chain authenticity by itself. The example combines it with `called` to require at least one result, but that cannot prove completeness, correct redaction, or absence outside the boundary. Verify and inspect the fixture separately. Older binaries may silently ignore the newer YAML assertion.

The CLI intentionally does not generate an assertion from a recurring group: even normalized text may include secrets, and a generated literal can be unstable or overbroad. A later opt-in workflow should require reviewed text and a separate evidence-eligibility check (at least matching recorded result, known integrity/completeness and redaction caveats), with compatibility-preserving engine semantics. The existing behaviour is unchanged.

## Verification

- Baseline and final Windows `go test ./...` passed, including M5.1 policy pack enforcement, replay, session/hash chain, historical workbench fixtures, M6 detector and assertion tests. Final `go vet ./...` passed; 23 Node frontend tests passed. `git diff --check` passed with line-ending advisories.
- CLI/web parity tests compare fresh summary counts and the sanitized core comparison fields; deterministic JSON is checked repeatedly. Focused tests cover empty/incomplete/invalid evidence, no matching calls, redacted data, MCP errors, multiple results, changed case, 125-file truncation, traversal, duplicate names, concurrent fresh scans, content-revision cache invalidation and generator overwrite refusal.
- Windows symlink creation failed because the account lacks the required privilege, so the focused link test was skipped; the inherited confined `os.Root`/regular-file reader was reused. No native Linux/macOS run or Go race-detector run was performed (`CGO_ENABLED=0`).
- Fictional end-to-end example: `tapelog verify` accepted the retry recording; the supplied `called` plus `result_never_contains` test failed as intended, identifying the literal error in four recorded results. No upstream tool ran. Existing files were unchanged when the generator was retried against an existing directory.
- On Windows/amd64, Go 1.27.3, Intel i7-10870H, one cold benchmark iteration over 100 fictional files/1,000 calls took 298 ms with 33,773,312 total allocated bytes and 330,921 allocations. Allocated bytes are not peak RSS. One run is not a performance guarantee; the 120-file/48 MiB analysis caps prevent extrapolation to larger libraries.

## Remaining work

Complete transport-neutral extraction of discovery, record normalization, aggregation and comparison, preserving one authoritative engine and web/CLI contract tests. Add stronger history/changed-group explanations and explicit schema compatibility tests before expanding JSON consumers. Qualify native symlink behavior, Windows race testing with an approved toolchain, other operating systems, concurrent file replacement and larger bounded libraries. Independently review the output metadata/privacy boundary and assertion eligibility. No release or deployment decision is made here.
