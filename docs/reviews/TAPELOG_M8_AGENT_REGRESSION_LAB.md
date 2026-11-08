# Tapelog M8 — Agent Regression Lab: partial implementation handoff

**Status: NO-GO / incomplete.** This is an isolated development candidate, not an accepted milestone or release. Baseline: `269ab1d34e3e468bdd39cae2b50fa4939ccc3f4e` on `feat/m7-behaviour-cli-and-workflows`. Candidate branch: `feat/m8-agent-regression-lab` in this isolated clone. The final candidate commit is the commit containing this report; use `git rev-parse HEAD` in this clone to identify it. Nothing was pushed, merged, or released.

## What is implemented

- Versioned, bounded case and suite JSON definitions; explicit leaf filenames and confined, size-limited reads. A case stores expectations, evidence filename, optional original SHA-256 revision, selected event numbers, and an optional reference. Generated cases do not copy recorded arguments or results.
- A conservative lab eligibility layer delegates supported assertions to the existing scenario engine. It requires chain verification, a completed recording, unambiguous call/result correlation and required evidence. Missing results cannot prove result-text absence. Recognised `[REDACTED` markers cause relevant text checks to become inconclusive. Unsupported expectation types are rejected.
- A visual preview and explicit save flow extends the existing test builder. It can check the selected recording or an explicitly named candidate and return to the selected event. No tool is executed by the lab. The browser save picker refuses a selected nonempty file; the download fallback uses a timestamped name, with browser download behaviour controlling collision handling.
- `tapelog lab run` and `tapelog lab suite` run cases offline. Suites enumerate case files explicitly and run in listed order. JSON is versioned. Exit codes: 0 PASS, 1 FAIL or invalid input, 2 INCONCLUSIVE. In a mixed suite FAIL takes precedence over INCONCLUSIVE. The CLI can request an M7 reference comparison for a single case; differences do not become failures without an assertion.
- Original `tapelog test` semantics remain unchanged. The stricter lab behaviour is opt-in. Results concern recorded MCP boundary events, not unobserved agent actions. An intact hash chain does not authenticate who produced a recording.

## Workflow example

Save a visual case alongside its recording. Alternatively, author a case explicitly, for example:

```json
{"version":1,"title":"Lookup was answered","evidence":"example.jsonl","expectations":[{"kind":"answered","tool":"demo__lookup"}]}
```

Run `tapelog lab run --dir ./evidence --case lookup.json --json`. To evaluate an explicit candidate, add `--candidate candidate.jsonl`; the candidate is verified afresh and does not have to match the original evidence hash. It is not a re-execution of historical actions. A suite named `suite.json` can contain:

```json
{"version":1,"title":"Lookup suite","cases":["lookup.json"]}
```

Run `tapelog lab suite --dir ./evidence --suite suite.json --json`. A practical local CI step is `tapelog lab suite --dir ./evidence --suite suite.json --json > lab-results.json`; configure the runner to retain the JSON artifact and fail the job for either nonzero status. In a GitHub Actions job where a trusted, independently installed `tapelog` executable and evidence files are already present, the corresponding step is:

```yaml
- name: Run recorded regression suite
  shell: bash
  run: tapelog lab suite --dir ./evidence --suite suite.json --json > lab-results.json
- name: Retain lab result
  if: always()
  uses: actions/upload-artifact@v4
  with:
    name: tapelog-lab-results
    path: lab-results.json
```

This snippet has not been executed in GitHub Actions and does not install or validate Tapelog. Hosted services are not needed by the core runner. For inspiration, [LangSmith documents offline evaluation and regression testing](https://docs.langchain.com/langsmith/evaluation-types), while [Promptfoo documents declarative test cases](https://www.promptfoo.dev/docs/configuration/guide/) and [machine-readable outputs](https://www.promptfoo.dev/docs/configuration/outputs/).

## Verification actually performed

- Windows: `GOPROXY=off go test ./...` passed, including existing policy, scenario, web and CLI packages; this run preceded the final added lab integration and tampered-evidence tests. Those new tests and their packages subsequently passed with `go test ./internal/lab ./internal/web -count=1`.
- Windows: `GOPROXY=off go vet ./...` passed. Existing frontend test suite passed 23 tests.
- Built the CLI and ran a fictional case pointing at a missing recording: JSON reported INCONCLUSIVE and the process exited 2.
- Added targeted lab tests for passing and failing result absence, missing result, redaction marker, incomplete session, duplicate correlation ID, tampered chain, missing target, revision mismatch, strict parsing, unsafe path, suite outcomes, and web preview payload/revision preservation.
- No browser walkthrough or screenshots were captured. No native Linux/macOS execution, live-client testing, accessibility audit, GitHub Actions run, vulnerability scan, or independent security review was performed for this candidate. Existing frontend tests do not specifically test the new lab UI. No claims are made that these gates passed.

## Release blockers and precise next steps

1. **Complete M7 extraction:** discovery, recording normalisation, aggregation, comparisons and CLI adapter still reside in `internal/web`; the lab CLI uses the web comparison adapter. Move shared analysis into a non-web package with parity tests before considering M8 complete.
2. **Complete the workbench:** add saved case/suite browsing, opening/editing, search/filtering, suite management and per-failure links to the exact evidence event, with keyboard and responsive walkthroughs and screenshots. Current UI creates a case with one selected rule; it does not open saved cases or manage suites. The CLI workbench link points to the session rather than a specific event.
3. **Finish comparison presentation:** expose new/resolved failures, denial and repetition changes, unanswered calls, denominators, and ambiguous/unmatched events in the lab UI, with navigation to evidence. Suite output currently has no comparison details. Comparisons are observational only.
4. **Complete evidence semantics and verification:** explicitly define and support repeated-failure expectations, validate all redaction representations (the current marker test is not a universal redaction detector), and expand tests for denied operations, sequences, blobs, malformed or duplicated events, symlinks, large suites, historical recordings, hostile contents, CLI/web consistency, and actual save collision behaviour. Inspect the legacy test builder wording so its weaker absence semantics cannot be mistaken for the new lab guarantee.
5. **Finish operational gates:** targeted frontend tests for the new controls, independent review of path and correlation safety, browser/keyboard/light-dark/small-screen checks, native platform and live-client checks where required, and a fresh vulnerability assessment. Existing M7/DX release blockers are not cleared by these tests.

A valid chain and an end event establish only the properties checked by this implementation; they do not prove complete observation of external behaviour or prevent a maliciously generated but internally consistent recording. Treat this branch as work in progress until the listed blockers are resolved and reviewed.
