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

Each JSON result is an object with `schema_version: 1`, `analysis`, `status` (`complete` or `partial`), `argument_result_text_included: false`, and `result`. The result reuses M6 fields for `counts`, `library`, `trend`, group IDs, references, and rates. Each rate supplies its recorded denominator; null means unavailable. In `compare`, `reference_n`/`current_n` are denominators for ratios; for `per_minute` they are call numerators and `reference_duration_ms`/`current_duration_ms` are the time denominators. `workbench_links` are suggestions, not proof a server is running.

**Array order is a data contract, not a formatting detail.** Every array of objects in this output is declared either producer-ordered (`conversations`, `timeline`, `repeats`, `failures`, `denied`, `tools`, `metrics`, the reference-comparison lists) or an unordered set (`trend`, `recent`, `skipped`, `failures.recent`). Producer-ordered arrays keep the order the analyser produced — a timeline read out of order is a different timeline — and only the declared sets are canonicalised for byte-stable output. An array whose meaning is not declared keeps its producer order by default, and `TestArrayOrderContract` fails when a new array of objects appears with no declaration, so a new collection cannot silently inherit the wrong behaviour.

**`counts.sessions` counts analysed files, not session identities.** `counts.identities` counts distinct non-empty `session_id` values across the analysed files; a single file can contain more than one, and a file with no identity contributes none. Each trend row carries `session_id` (the first event's id) and `structure` (`valid`, `invalid`, `incomplete` or `unreadable`).

**`complete` now means the recording is one valid session log with an observed end.** It is true only when the file has a single `session_id`, exactly one `session/start` first, and exactly one `session/end` last. A structurally invalid file (mixed session ids, missing or repeated boundary, non-last end) is `partial` with `structure: "invalid"`, and an open recording is `partial` with `structure: "incomplete"` — never `complete`, and never reported as a completed conversation.

**`complete` is independent of `integrity`.** `complete` describes structure and boundary only; `integrity`/`chain_ok` describe the hash chain. A file can be `complete: true` with `integrity: "invalid"` and `chain_ok: false` — a tampered recording can still be structurally whole. Read `integrity` before trusting `complete`. An empty file is `integrity: "empty"`, `chain_ok: false`, `structure: "invalid"`: it has no chain at all.

The CLI omits error message labels, literal fragments, error codes, policy reasons, session task names and harness strings because normalization does not guarantee removal of secrets. Tool names, rule IDs, server names, selected filenames, timestamps and event IDs remain visible and can themselves be sensitive metadata. The call comparison may include argument/result hashes; hashes reveal equality and can be guessable for low-entropy values. Treat output as potentially sensitive. No raw argument or result payload is emitted.

Directory discovery considers at most 200 newest regular `.jsonl` files from the first 10,000 directory entries; analysis is bounded to 120 files, 48 MiB total and 16 MiB/50,000 events per file. The JSON coverage object identifies caps, truncation and skipped files. Explicit names are bounded to 120 per request. Workbench cache reuse verifies the current file bytes and revision first; a freshly invoked CLI uses its own cache. Analysis is restricted to recorded data, and a chain check is consistency evidence, not proof of authorship, completeness or unrecorded actions.

## Conversation-scoped investigation (M14)

A conversation is the unit a developer usually investigates, so two commands read it directly instead of asking them to page through a library summary.

```powershell
tapelog behaviour conversation --dir fixtures --conversation conv-1
tapelog behaviour conversation --dir fixtures --conversation conv-1 --offset 200 --limit 200
tapelog behaviour compare-conversations --dir fixtures --a conv-1 --b conv-2
tapelog behaviour conversations --dir fixtures --json          # the whole library, still available
```

`behaviour conversation` prints the conversation's counted facts, the evidence limits that apply to it, and one page of its **library-ordered** timeline (recordings in the order this view analysed them, then sequence within each recording). `--offset`/`--limit` page through every recorded call, including conversations longer than the overview's 200-entry cap; the command always states how many calls were shown out of how many.

`behaviour compare-conversations` compares two conversations on recorded counts only. It prints a metric table (calls, results, unanswered calls, tool errors, denials, ambiguous calls, repetition findings), the tool-usage difference and the repetition difference, then two separate lists:

- **observed differences** — one factual sentence per non-zero change, produced server-side so the terminal, the API and the card cannot drift apart (*"The second conversation recorded 3 more tool errors than the first"*), never a character judgement;
- **limits of this comparison** — ambiguity, unanswered calls, different recordings, truncated timelines, and the standing rule that a difference is an observation about recorded boundary activity.

It refuses to compare a conversation with itself, and it refuses any pair where one side is not an attributable conversation: those are answers (`comparable: false` with an `explanation`), not errors. `--a`/`--b` must be plain ids (`A-Za-z0-9._:-`, 1–128 bytes): the reserved `(unattributed)` label, or any value that is not a valid id, is refused as a bad request with the identifier rules rather than treated as a group.

`behaviour conversations --conversation <id>` narrows the **text** view only; with `--json` it refuses and points at `behaviour conversation`, which is the command that returns one conversation's versioned payload. That is deliberate: a library-wide payload with its counts intact but its group list silently filtered would be a payload that disagrees with itself.

The HTTP equivalents are `GET /api/behaviour/conversation?conversation=<id>&offset=<n>&limit=<n>` and `GET /api/behaviour/compare-conversations?a=<id>&b=<id>`, with the same window filters as `/api/behaviour`. A conversation id that no analysed recording declares returns `found: false` with the reason and the library coverage, rather than an empty conversation. Ids are validated on read as well as on write, so a hand-edited recording cannot forge membership and an id carrying control characters never reaches a terminal or a URL.
