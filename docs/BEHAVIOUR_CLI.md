# Behaviour workflows (M7)

The commands inspect recorded MCP-boundary events; they never launch a server or execute a recorded tool. Run from the repository root. The generator **requires a new directory** and refuses an existing one, so choose a new path for each run.

```powershell
go run ./tools/gen-m6-demo m7-demo-sessions
tapelog behaviour summary --dir m7-demo-sessions
tapelog behaviour repeats --dir m7-demo-sessions 02-retry-loop.jsonl
tapelog behaviour failures --dir m7-demo-sessions 02-retry-loop.jsonl
tapelog behaviour compare --dir m7-demo-sessions --reference 01-baseline-release.jsonl --candidate 04-denials.jsonl
tapelog behaviour failures --dir m7-demo-sessions --json 02-retry-loop.jsonl > failures.json
```

The full demo library contains an intentionally unfinished recording, so the first command prints partial coverage and exits 2. An explicit complete recording, as in the next commands, can exit 0. A repeated call or newly observed denial does not itself fail an analysis. `complete` describes analysis coverage and verified recording endings; it does not mean every call has a result. Errors go to stderr; result JSON goes to stdout. Exit 0 means the requested set was analysed without known incompleteness, 2 means partial or no evidence, and 1 means the command or input was invalid. Verify the exit status in automation before treating a result as complete.

Output references plain `.jsonl` filenames, call sequence numbers and suggested loopback links. To use the links, separately start `tapelog web --dir m7-demo-sessions` with its default loopback address; no command opens a browser or starts the dashboard. A link is meaningful only for the same directory and current recording contents. Filenames are relative to `--dir`, cannot traverse directories, and explicit symlinks are rejected.

A fictional supported test is in `examples/behaviour-workflow/error-absence.yaml`. After generating the demo above:

```powershell
tapelog verify m7-demo-sessions/02-retry-loop.jsonl
tapelog test --plain examples/behaviour-workflow/error-absence.yaml
```

The test deliberately **fails** on that fixture because the recorded error is present. It combines `called` with `result_never_contains`: `called` requires a matching recorded result but does not establish recording completeness or guarantee that an error was not redacted. Confirm the recording's completeness and integrity separately. A literal, case-sensitive absence assertion passes if there are no matching results, if relevant text is redacted, or if the error wording changes. It scans both successful and error result JSON, not unrecorded effects. Older Tapelog binaries may ignore this assertion; use a binary that recognises it. No regression-test proposal is generated from failure text automatically because a normalized message is not a reliable secret filter or a guaranteed stable literal.

## JSON contract, version 1

Each JSON result is an object with `schema_version: 1`, `analysis`, `status` (`complete` or `partial`), `argument_result_text_included: false`, and `result`. The result reuses M6 fields for `counts`, `library`, `trend`, group IDs, references, and rates. Each rate supplies its recorded denominator; null means unavailable. In `compare`, `reference_n`/`current_n` are denominators for ratios; for `per_minute` they are call numerators and `reference_duration_ms`/`current_duration_ms` are the time denominators. JSON arrays of object groups are sorted deterministically, not necessarily in the UI's ranking order; trend rows stay chronological and scalar event sequences retain their order. `workbench_links` are suggestions, not proof a server is running.

The CLI omits error message labels, literal fragments, error codes, policy reasons, session task names and harness strings because normalization does not guarantee removal of secrets. Tool names, rule IDs, server names, selected filenames, timestamps and event IDs remain visible and can themselves be sensitive metadata. The call comparison may include argument/result hashes; hashes reveal equality and can be guessable for low-entropy values. Treat output as potentially sensitive. No raw argument or result payload is emitted.

Directory discovery considers at most 200 newest regular `.jsonl` files from the first 10,000 directory entries; analysis is bounded to 120 files, 48 MiB total and 16 MiB/50,000 events per file. The JSON coverage object identifies caps, truncation and skipped files. Explicit names are bounded to 120 per request. Workbench cache reuse verifies the current file bytes and revision first; a freshly invoked CLI uses its own cache. Analysis is restricted to recorded data, and a chain check is consistency evidence, not proof of authorship, completeness or unrecorded actions.
