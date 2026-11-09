# Changelog

All notable changes to tapelog are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

**Status:** **[0.4.0]** is the latest tagged release (2026-10-01).
[`internal/buildinfo`](internal/buildinfo/buildinfo.go) is the single
source of truth for the version.

## [0.5.0-beta.1] — public beta

- **`tapelog demo` — the first-run experience a downloaded package can actually have (BETA)** — one
  command records a complete fictional session through the real boundary (an allowed call, a
  policy-denied call and a tool error) against the fixture the compatibility tests use, then verifies
  the chain it just wrote and prints the inspect / workspace / Regression Lab commands. It needs no
  agent, account, network or source checkout, and it refuses to overwrite an existing directory rather
  than silently replacing evidence.
- **Archives now ship the guides and the suite they depend on (BETA)** — `docs/GETTING_STARTED.md`,
  `docs/SECURITY-AND-LIMITATIONS.md` and `docs/REGRESSION-LAB.md` travel with the binary, next to the
  fictional `examples/regression-demo/` recordings, so the documented walkthrough runs from the
  download alone.
- **Installation is archive-first, with the privacy guidance before the first recording (BETA)** —
  Getting Started leads with download, checksum verification and `tapelog demo`; the privacy block
  states what recordings contain, that redaction is bounded, what omit mode costs in replay fidelity,
  that beta users should start with fictional data, and that nothing is sent anywhere.
- **Two platforms for the beta, and only two (BETA)** — Windows amd64 and Linux amd64 are built,
  executed and walked through. macOS and linux/arm64 compile and are deliberately not offered.
## [Unreleased]

- **Observed-group rows now lead with the conversation they represent (design)** — the user-agent
  disclosure marker became a *grid item* once the summary was laid out as a grid, so it claimed the
  first `minmax(0,1fr)` track and pushed the identifier, badges and counts into the last two columns:
  a tiny marker sat at the far left with a ~700px empty gap before the name. The marker box is now
  removed from the grid and replaced by an explicit chevron that shows the open state; the row reads
  chevron → client-declared id → counts, with `Open →` consistently at the right edge. Long ids
  truncate with the full value in `title`, the unattributed remainder keeps its own label and no
  Open action, and the phone layout puts the counts under the identifier. Measured at 320/390/1440.

- **The session screen now states the structure verdict, not only the chain (evidence integrity)** —
  a recording with an intact hash chain but an invalid structure (mixed identity, no first
  `session/start`) was presented as merely "no session/end": the trust panel read the chain, the
  completeness flag and the end reason, but never `structure` or `structure_problems`, so the one
  surface where a reader judges the evidence described a structurally invalid file as incomplete.
  It now emits a separate error banner naming the recorded problems (for example
  `mixed_identity at seq 9`) and says plainly that integrity and structure are different questions.
  `no_end` stays with the completeness banner instead of being repeated. Found by the independent
  reviewer, fixed with a rendering test watched failing first.

- **Accessibility acceptance pass on the Prism workspace (design)** — measured in the real browser with
  axe-core 4.14 across seven screens in both themes: `--text-4` was 3.78:1 and is now 5.62:1 dark /
  5.31:1 light; form boundaries take a dedicated `--control-line` (≥3:1 on every surface); a focused
  field now paints an opaque `--focus` ring instead of a 12%-alpha tint. Structured semantics replaced
  visual-only fixes: glossary entries are real `<dl>` lists, the policy source controls are a labelled
  group rather than a `role="list"` holding buttons, an activity row's accessible name comes from its
  visible text instead of a shorter override, the conversation `Open →` action moved beside its
  disclosure instead of inside the `<summary>`, the duplicate search glyph was removed, and selection
  checkboxes are a 24px target. Seven new rendering/contrast regression tests were each watched fail
  before their fix; the frontend suite is 57 pass.
- **Behaviour notes now name structurally invalid evidence (evidence integrity)** — a recording whose
  chain is intact but whose structure is invalid (mixed identity, no first `session/start`) was counted
  in the overview totals with no note at all; the notes named only a failed hash chain. It now emits a
  separate warning that those totals include evidence which cannot establish one complete session.

- **Prism design coverage: every class the UI emits now has a rule (design)** — the replacement
  stylesheet was written against a class vocabulary `app.js` does not use, so ~57 component classes
  fell back to user-agent layout. The stylesheet now covers all 218 emitted class names (route words
  and the dynamic `finding-` prefix excepted): the call inspector (evidence card, policy steps, call
  actions), the all-events list, comparison verdict/pair/diff groups, behaviour deltas, repeat and
  failure rows with per-kind severity (a repeated tool error is red, a repeated denial amber), the
  policy studio's rules, conditions and simulation rows, the case builder's selected sequence, the
  Lab's expectations and case JSON, and the conversation view. Six confirmed layout defects were
  measured before and after in the browser: the context bar concatenated its three identity lines,
  `.tool-meta` ran `Not recorded9 Nov, 16:45Policy: allow` together, `.tool-outcome` ran
  `Result recorded1 ms` together, `.activity-row` sized itself to 385px inside a 614px list, compare
  labels touched their selects, and the footer read `…view.Evidence, with context.`
- **Two overflow defects found by measurement and fixed (design)** — a policy-base select sized its
  grid column from its longest option and pushed a 320px viewport 75px sideways, and a long mono
  call key in the compare policy history pushed a 390px viewport 187px sideways. Builder controls
  are now `width:100%` with `min-width:0`, and mono identifiers wrap. Verified across 49 screen ×
  width combinations: no page overflow and no element spilling text.
- **`stats()` guarantees its own strip (design)** — the candidate summary rendered four `.stat`
  blocks stacked full-width because it never received `.stat-strip`; the helper now adds the class
  to any container that lacks it.

- **Prism completion pass: every implemented screen art-directed (design)** — the policy studio,
  saved investigations, comparison, Regression Lab and the approvals queue were still falling back
  to user-agent layout. Policy sources are now a hairline list with a selected edge, rule action
  chips have real styles (the `.chip` classes had none), rules expand into a fact grid, the studio
  reading column repeats which source it explains, comparison opens with a `Differs` / `No change`
  status word and shows its three difference groups without a disclosure, lab case rows are rows
  with an accent selection edge, and detail panes are rails rather than rounded cards.
- **Measured accessibility corrections (design)** — table column labels and `.finding-label`
  measured 3.87:1 dark / 3.62:1 light at 10.5px and now take the metadata grey (5.95 / 5.01);
  `.source-item small` on a selected row measured 4.39:1 light and now takes `--text-2`. Behaviour
  at 320px overflowed by 12px because a grid item carried `margin: 0 auto` (which turns off grid
  stretch); the session integrity disclosure was a flex sibling of its summary and collapsed its
  banner to 34px; timeline entries were appended as the only grid child and rendered inside the
  52px sequence gutter. Tap targets under 24px (`back`, `link-button`, timeline links) were raised.
- **Regression Lab copy and labels (design)** — pluralisation for counts (`1 suite`, `1 finding`,
  `1 deny decision`), and the candidate picker now always shows the recording file, because one
  harness can produce several recordings with the same name.
- **Fictional review library (local, not committed)** — a second, independently generated set of
  seven recordings, nine cases and two suites in a disposable local review folder,
  written through the real boundary and re-asserted on every regeneration: 4 PASS, 1 deliberate
  FAIL, 4 INCONCLUSIVE (`incomplete_recording`, `structure_invalid`, `conversation_absent`,
  `ambiguous_correlation`).

- **JSON carried as a tool result is redacted by key (M15.1)** — an MCP result usually delivers its payload as a
  string (`content[].text`), so a sensitive key inside it was stored verbatim even though the argument copy of the
  same value was masked. A JSON document returned as text is now parsed and masked with the same key rules as
  arguments, including nested objects, arrays and JSON escaped inside JSON. The behaviour is bounded and stated:
  256 KiB, 24 nested string layers, one well-formed document; anything else is left to the credential-pattern pass
  and recorded as received. Text that nothing matched is kept byte-identical rather than re-serialised.
- **`--omit-result-text` records the shape of a result and none of its bytes (M15.1)** — for output that cannot be
  safely redacted (prose, malformed or oversized payloads). Content types, ids and `is_error` survive; payload text
  becomes `[REDACTED] (result text omitted at record time)` before hashing, so the bytes never reach the disk and
  replay reproduces exactly what was kept. Result-based regression claims report `redacted_content`.
- **Two disclosures removed from the shipped tree (M15.1)** — `scripts/gen-third-party-notices.py` hard-coded one
  machine's checkout path (now derived from the script location), and the example plugin's `argguard.wasm` carried
  the builder's home directory in its debug info (rebuilt with `-trimpath -ldflags="-s -w"`, and `build.ps1` now
  does the same). `examples/roo-live-scenario.yaml` no longer asserts on an absolute path.
- **The release candidate identifies itself (M15)** — a source build reported `0.4.0`, the same string as the
  published release, so no banner could tell them apart. `internal/buildinfo.Version` is now `0.4.1-m15rc.1`, and
  `Commit` is stamped through `-ldflags` and printed beside it: `tapelog 0.4.1-m15rc.1 (286165e)`. A plain
  `go build` prints no commit — the absence is the signal that the binary carries no build provenance. The
  published `v0.4.0` tag is untouched.
- **Local release-candidate packaging (M15)** — `scripts/build-release-candidate.sh` builds the five configured
  targets `CGO_ENABLED=0` with `-trimpath`, stamps version and commit, bundles `LICENSE`, `README.md`,
  `CHANGELOG.md`, `THIRD_PARTY_NOTICES.md` and the fictional `examples/regression-demo/` suite, and writes
  `checksums.txt` plus a manifest that states signing and SBOM production were **not** performed. Building it
  exposed a real defect: MSYS `tar` recorded `0644` for every file on NTFS, so the Linux and macOS archives shipped
  a binary that could not run. Archives are now written with Python `tarfile`, and the Linux archive was extracted
  and executed in a clean container.
- **Third-party notices are generated (M15)** — `scripts/gen-third-party-notices.py` reads the module cache and
  writes `THIRD_PARTY_NOTICES.md` (122 modules, 0 unclassified). `github.com/mattn/go-localereader` ships no licence
  file inside its module zip; its upstream repository is MIT (Copyright 2022 Yasuhiro Matsumoto), and the notices
  record it as a reconstructed entry rather than a guess.
- **What redaction actually masks is now measured, not assumed (M15)** — nine recording shapes were written and the
  raw bytes inspected: argument values under sensitive keys are masked, and credential shapes (AWS keys, GitHub
  tokens, `Bearer` after a colon) are masked wherever they appear, including result text. Sensitive key names inside
  a result body are **not** consulted — `{"password": "…"}` in a text result is stored verbatim, in every form,
  including malformed and 1 MB bodies. No configuration omits result bodies, so the candidate is documented as
  fictional-data-only in `docs/GETTING_STARTED.md` and `docs/SECURITY-AND-LIMITATIONS.md`.
- **A policy decision now attaches to the call it decided, not to whichever call was oldest (M15)** — when two
  calls shared a request id while both were outstanding, the loader gave the second call's decision to the first,
  so a Regression Lab case asserting "read_file was denied" returned **PASS** on a recording where `read_file` was
  allowed and only `delete_file` was denied. Decisions now attach by `parent_seq` (which the recorder has always
  written), a verdict attached by id alone is marked as a guess and cleared if the id turns out to have been
  shared, and `denied` eligibility returns `ambiguous_correlation` instead of reading an absence as "not denied".
  Found by independent review; the same case now FAILs where the pairing is recorded and stays INCONCLUSIVE where
  it is not.
- **Sensitive field names are matched after normalisation (M15)** — the list was matched as a substring of the key
  as written, so `passphrase`, `privateKey`, `accessKey` and `session_key` were recorded verbatim while
  `password`, `private_key` and `api_key` were masked. Keys are now lowercased with `_`/`-`/`.` removed before
  matching, and the list names the same secret under its other words (`passphrase`, `session_key`, `auth_token`).
  A second test pins that ordinary keys (`path`, `author`, `session_id`, `hostname`) stay readable.
- **Replay serves the bytes it verified (M15)** — `LoadVerifiedAttributable` verified one read of the file and then
  re-opened the path to parse it, so a file swapped in between would be served without passing either gate. It now
  parses the verified buffer.
- **Every replay override now announces itself (M15)** — `--allow-ambiguous-ids` served evidence with shared,
  still-outstanding request ids and said nothing, while its two siblings (`--allow-unverified`,
  `--allow-invalid-structure`) each print a `WARNING` on stderr. A caller piping replay into a harness could not
  tell that a served answer might belong to the other call. It now names the ambiguous ids, and
  `TestReplayAmbiguityOverrideIsVisible` pins both the warning and the default refusal's pointer to the flag.
- **The lab's event-limit refusal no longer reads as corruption (M15)** — the workbench glossed a
  `damaged_recording` refusal as "damaged or ambiguous" even when the recording was merely above the 10,000-event
  lab limit; it now says so.
- **Performance re-baselined on the integrated candidate (M15)** — no regression of the M12/M13 work: the block
  detector's pathological identical run is 0.65 ms with a single allocation (the fix it replaced measured 799.6 ms),
  and conversation grouping remains single-pass at 9.4 ms for 12 000 conversations. Recording sustains about
  1 040 calls/s. One cost is documented rather than optimised: on an 11.9 MB recording a repeat `/api/behaviour`
  call is ~830 ms against 959 ms cold, because the request re-reads and re-decodes the file. See
  `docs/releases/M15_PERFORMANCE_RELIABILITY.md`.
- **Redaction limits are now stated and pinned (M15)** — key-based masking covers JSON objects and known
  credential shapes, but it does **not** reach JSON that a server returns as text, which is the usual shape of an
  MCP result: a server that echoes arguments writes the secret into the result payload even though the argument copy
  is masked. `docs/THREAT_MODEL.md` and `docs/SECURITY-AND-LIMITATIONS.md` now say so, ADR 0003 no longer claims the
  log never contains raw secrets, and `TestRedactionGapJSONEncodedStrings` pins the gap so closing it forces a doc
  update in the same change.
- **The published gateway evidence is derived, not the original bytes (M15)** — the authentic M14.1 recordings
  carried the recorder's absolute policy path. `test-fixtures/m14.1-gateway/` now holds re-chained copies with a
  neutral policy name, `DERIVED.md`/`DERIVED.json` record both sets of heads, and the test fails if a derived file
  ever shares the original's head. The originals stay outside the public tree.
- **The gateway attribution evidence ships with the repository (M15)** — the M14.1 live session is committed as
  two byte-linked snapshots (`test-fixtures/m14.1-gateway/`): the 16-event file verified during the experiment
  (chain intact, **INCOMPLETE — no_end**) and the 17-event file the gateway completed eleven minutes later. The open
  file is an exact byte prefix of the closed one, so the difference between "still recording" and "finished" is a
  verifiable fact rather than a footnote. `TestGatewayEvidenceIsVerifiableAndHonest` asserts the chain, the
  completeness split, five calls across two client-declared conversations, zero unattributed calls and the single
  `no-delete` deny. `docs/HERMES-ATTRIBUTION.md` separates Tapelog's ability to ingest conversation metadata from any
  particular client supplying it, and states that unmodified clients send nothing.
- **Documentation a stranger can follow (M15)** — `docs/GETTING_STARTED.md` is now the entry point: build from
  source, identify which binary you actually have (the version banner cannot tell a source build from v0.4.0), run
  the ~0.9 s offline demo, wrap one existing server, then reach the workbench and the lab. It states the tested
  platforms rather than implying support: Windows amd64 accepted, Linux amd64 partially (race suite in Docker),
  macOS built but not functionally accepted. `docs/REGRESSION-LAB.md` documents the claim kinds, the three verdicts,
  the two eligibility modes and the 10,000-event limit, none of which had user-facing documentation.
  `docs/SECURITY-AND-LIMITATIONS.md` separates authoritative mediation from passive observation and links the
  threat model rather than restating it. `docs/launch/TESTING-GUIDE.md` no longer hardcodes the author's checkout
  path and Go installation path.
- **`tapelog web` binds before it announces (M15)** — the command printed `session dashboard on http://…` and only
  then called `ListenAndServe`, so an occupied port produced a success-looking URL followed by a bind error, and any
  script grepping for the URL received the address of a server that never ran. It now listens first, reports the
  address the listener actually got (so `--listen 127.0.0.1:0` prints the real port), and fails with only the error.
  Covered by `TestWebDoesNotAnnounceBeforeBinding`.
- **The workspace stops hiding its own warnings (M15)** — the integrity, missing-end and ambiguous-request
  banners lived inside a collapsed "Recording integrity & scope" disclosure, so a session whose result pairing
  cannot be determined looked like two plainly unanswered calls. The disclosure now opens itself when any warning
  was rendered. Library rows also carry a `pairing unknown` badge (the API already reported `ambiguous`), and the
  card count reads `1 call` rather than `1 calls`.
- **Comparison is refused where the evidence cannot support it (M15)** — the ambiguity banner claimed
  "Comparison and test creation are disabled" while both stayed clickable (the API refused the comparison, the UI
  did not). Compare is now actually disabled for an ambiguous recording, and the sentence states what happens
  instead: a result whose owner is unknown is attached to neither call, and a case that depends on which call
  produced it will be inconclusive. Test creation stays available on purpose — the lab decides per claim.
- **A replay override is now visible (M15)** — `tapelog replay --allow-invalid-structure` overrode the structure
  refusal and then said nothing: stderr showed `replaying <file> (session , 0 interactions)` with no warning, while
  `--allow-unverified` had always printed a loud one. It now prints `WARNING — replaying evidence that is NOT one
  complete valid session (<code>)`, naming the shared checker's own problem (`no_events`, `no_start`, `no_end`, …), so it
  agrees with `verify` and `inspect`. Covered by `TestReplayStructureOverrideIsVisible`, which also pins that the
  default refusal names the flag. No eligibility rule or refusal was changed.
- **A five-case fictional regression demo ships in `examples/regression-demo/` (M15)** — one complete session with two
  client-declared conversations produced through the boundary, plus an interrupted copy, and five cases that produce
  PASS, FAIL and INCONCLUSIVE deliberately (2 pass / 1 fail / 2 inconclusive). `generate.sh` rebuilds both recordings
  and asserts the defects survive generation. No Hermes integration is needed to run it.
- **Structure verdicts now name the defect they found (M15)** — `tapelog inspect` printed one canned
  sentence for every structurally invalid recording, so a zero-event file was described as having
  "mixed session ids" while `verify` and the workbench API named `no_events`. The CLI now renders the
  shared checker's own codes (`no_events`, `no_start`, `start_not_first`, `mixed_identity at seq N`,
  `multiple_starts`, `no_end`) and the workspace API exposes them as `structure_problems`, so every
  surface names the same problem. Covered by `TestStructureVerdictNamesTheActualDefect`, which also
  asserts the empty file is *not* described as mixed identities.
- **M14 review fixes** — a lab conversation scope now treats a call whose declared id the analysis layer
  rejects as unplaceable (`conversation_scope_unknown`), closing the one way a scoped absence claim could
  PASS while such a call sat in the recording; the comparison's explanation field is `explanation` so it
  survives the CLI's payload minimiser; `behaviour conversations --conversation` validates its id (the
  remainder is not selectable) and refuses `--json` instead of silently ignoring the filter; a conversation
  now carries its recording's own warnings (integrity, completeness, unpaired results, unknown-source
  decisions); recorded timestamps are sanitised before printing; `recurrence` is declared in the array-order
  contract and the gate covers a real recurrence and the recording comparison; an invalid deep link clears
  the previous comparison; note and sentence grammar corrected.
- **The conversation is now the unit of investigation (M14)** — `tapelog behaviour conversation --dir … --conversation <id>`
  prints one conversation's counted facts, the evidence limits that apply to it and a paged, library-ordered timeline
  that reaches past the overview's 200-entry cap; `tapelog behaviour compare-conversations --a <id> --b <id>` compares
  two conversations on recorded counts only, with server-written factual difference sentences and a separate list of
  what the comparison cannot support. The workspace gains the matching deep link and panels. New endpoints:
  `GET /api/behaviour/conversation` and `GET /api/behaviour/compare-conversations`.
- **Array order is now a declared data contract, not an exemption list (M14)** — `workflowSafe` used to canonical-sort
  every array of objects except the names on a list, and forgetting the list has now produced three separate defects
  (conversation groups, repeats, timeline). The default is inverted: an undeclared array keeps the producer's order and
  only declared unordered sets are canonicalised; the contract is keyed by collection with path overrides, and
  `TestArrayOrderContract` fails when a new array of objects appears with no declared meaning.
- **Conversation counts are named for what they count (M14)** — `ambiguous_results` is `ambiguous_calls` (one unpaired
  ambiguous result shared by two calls was being reported as two "results"), a group now carries `unanswered`, per-tool
  call counts (`tool_calls`) and a library position (`first_library_seq`), and the overview states whether conversation
  order is `library` or `per-recording` instead of implying one chronology.
- **Two M13 evidence gaps are closed (M14)** — a recorded result that no call claims is now counted (`unpaired_results`,
  plus a warning) instead of leaving a conversation that looks complete while evidence is missing, and the CLI text view
  prints the ambiguity, unanswered and timeline detail the card already showed.
- **Unknown-source uncertainty is visible where it matters (M14)** — recorded policy decisions whose reason names an
  unknown value source are counted (`unknown_source_decisions`) and explained in the notes: the policy engine keeps
  provenance uncertainty for the rest of the session, so a later decision can rest on activity the view does not contain.
  It means provenance is uncertain, not that anything was contaminated, and the fail-closed behaviour is unchanged.
- **Conversation view in the workspace (M13)** — the Behaviour view opens with a CONVERSATIONS card built from
  the server's analysis: each declared conversation shows its calls, denials, errors, tools, repetitions
  measured inside that conversation, and a bounded timeline whose entries open the underlying event. The
  unattributed remainder is a separate group under its own heading, ordered last and never given a made-up id;
  ambiguity is shown as unknown rather than zero; a call with no recorded result is drawn as neither success
  nor failure. `CallRecord` gains `File`/`Index`, and a conversation summary gains `Files`, a 200-entry
  `Timeline` and `TimelineTruncated`.
- **`detectBlocks` is linear on long identical runs (M13)** — it measured a run's length before discarding
  single-operation blocks, so a polling tool repeating one call 8 000 times took 800 ms. The discard test now
  runs the moment the block is known to repeat: 1.75 ms at 8 000 calls, with output proven identical to the
  original algorithm across 240 randomised recordings.
- **Fixed: "1 calls were never answered"** — the unanswered-calls note now agrees with its number.


- **Conversation-aware Behaviour Intelligence (M12)** — a single MCP session can carry several agent
  conversations, and Behaviour Intelligence had only the session as an axis, so four conversations that each
  called `read_file` twice read as one eight-call loop. Calls now carry the client-declared conversation id;
  `behaviour conversations` groups activity per conversation with the unattributed remainder kept separate and
  last; recurrence (an operation seen in two or more conversations) is distinguished from repetition (the same
  operation inside one); and repeat findings record `scope: "conversation"` so existing `repeats`/`summary`
  output stops presenting cross-conversation recurrence as a single loop. Attribution never creates a session
  boundary, changes a verdict, alters hash-chain verification or affects Regression Lab eligibility, and it is
  labelled as client-declared wherever it is shown.
- **Unknown-source taint is fail-closed and durable (M12)** — past 4 096 distinct tool names the mediator
  cannot name a source, and a value from it is now marked as having an *unknown* source rather than no source.
  Value-flow rules treat an unknown source as matching, so the check a rule was written to make still happens.
  The mark is a sticky session fact, not a value in the capped store: a value-bucket eviction can no longer turn
  "never clean" back into "clean".
- **Fixed: producer-ordered arrays were silently re-sorted** — `workflowSafe` canonical-JSON-sorts arrays of
  objects for deterministic output, which rewrote conversation groups (unattributed first), recurrence and the
  ranked `repeats`/`failures`/`denied`/`tools` tables. Those arrays are now exempt.
- **Fixed: grouping was O(n·g)** — `GroupConversations` rescanned the call list per group (1 146.9 ms at 12 000
  conversations); it now buckets in one pass (5.95 ms).

- **Regression Lab is claim-specific about ambiguous evidence (M11.2)** — a recording that reuses a request id
  is no longer rejected outright as `damaged_recording`. Claims that need a result tied to a call (`answered`,
  `result_contains`, `result_never_contains`, `no_deny_bypassed`) become INCONCLUSIVE with
  `ambiguous_correlation`; claims that only need calls or decisions (`attempted`, `no_attempts`,
  `attempt_ceiling`, `denied`, `sequence`) stay decidable. Same rule in complete-boundary mode. Ambiguity is
  never upgraded to PASS
- **A denied call no longer leaks correlation state** — every deny path settles its own link (`dropLinkRef` via
  a named-return `defer`), and the loader decrements its outstanding count on a deny decision. Previously a
  denied call left a phantom "still outstanding" entry, so the next call under the same id looked ambiguous
  for no reason
- **`tapelog` records a response that follows a deny under a shared id** — the boundary deleted the whole
  in-flight entry for an id when denying, which hid an allowed call's response from the recorder (the client
  saw two responses, the recording kept one). It now decrements one slot
- **Taint refuses to under-taint at its own limits** — an overflowing per-id union no longer drops source
  names (which silently disabled value-flow rules); it falls back to every tool name seen this session.
  Ambiguity reporting is no longer capped at 16 ids internally, so an id past the cap is still labelled
- **Race-tested** — the full suite passes under `CGO_ENABLED=1 go test -race ./...` in a `golang:1.27`
  container with gcc (27 packages, no data races)

- **Request identity: no more manufactured certainty when ids collide (M11.1)** — a JSON-RPC id is only a
  correlation token while it is unique among *outstanding* calls. When two calls share an id, the transport
  cannot say which response answers which call, so `tools/result` now carries `attribution_ambiguous: true`
  and claims no `parent_seq`, `conversation_id`, `tool_call_id` or `traceparent`. The flag is sticky for that
  ambiguous cluster (it clears when the cluster drains, so later sequential reuse is exact again). M11's FIFO
  pairing presented a guess as fact and is replaced
- **Two evidence-loss defects fixed** — `internal/proxy` tracked in-flight calls in a boolean map and deleted
  the id on the first response, so a reused id's second response reached the client but was **never recorded**;
  it is now a per-id count. And a call's correlation link was registered *after* its event was appended, so a
  concurrent response could see a short queue and be declared unambiguous
- **Value taint degrades conservatively under collision** — `pendingTools` is a FIFO queue per id and a shared
  id taints every one of its results with the **union** of the tools that shared it (over-taint, never
  under-taint, deduplicated and capped at 32). The same fix applies to the memory caps: a cap wipe now retains
  discarded sources as always-assumed taint, and sets `corrLost` so no later pairing is attested
- **Decisions point at their own call** — a `policy/decision` no longer resolves through the queue head, which
  made decision #2 point at call #1 under a reused id
- **`replay` refuses ambiguous evidence** — `replay` exits non-zero naming the ids whose responses cannot be
  paired, with `--allow-ambiguous-ids` as the explicit escape hatch. Sequential reuse is still served normally

- **Conversation attribution (M11)** — one MCP connection can serve many agent conversations; the recorder can
  now group them without inventing session boundaries. A client MAY declare `_meta` keys
  `io.tapelog/conversationId` / `toolCallId` / `subagentId` (alias `io.hermes.agent/...`), recorded as additive
  `conversation_id` / `tool_call_id` / `subagent_id` on `tools/call`, `policy/decision` and `tools/result`.
  Client-declared correlation only: `session_id`, structural validity, completeness and `lab` eligibility are
  untouched, and absent attribution is reported as absent rather than invented. Identifiers are bounded
  (1–128 bytes, `[A-Za-z0-9._:-]`) and rejected rather than truncated. See `docs/CONVERSATION-ATTRIBUTION.md`
  and spec §6.3
- **`inspect` shows the conversation** — a call carrying attribution prints `[conversation <id>]` beside it; an
  unattributed call prints nothing rather than a placeholder
- **`replay` matches repeated request ids in order** — a long-lived recording may legitimately reuse JSON-RPC
  ids (a client that restarts its counter each conversation). Matching was last-write-wins, which silently
  dropped the earlier call and could pair a result with the wrong request: a two-call recording was served as
  one. Matching is now FIFO per id, and decisions may still supersede one another (decision history)
- **Hermes integration available as a patch** — an isolated, reviewable two-file change forwards the
  conversation and tool-call ids as MCP `_meta`, and is skipped entirely when the installed SDK's `call_tool`
  has no `meta=` parameter
- **Evidence integrity: four properties, never conflated** — `verify`, `inspect`, `behaviour`, `replay` and `lab`
  now separate *cryptographic integrity*, *structural/session validity*, *completeness* and *per-assertion
  eligibility*. One shared checker (`internal/session.InspectStructure`) defines what a well-formed session log
  is: ≥1 event, a non-empty `session_id` on every event, exactly one identity, exactly one leading
  `session/start`, and at most one `session/end` which must be last. A missing end is **incomplete**, never
  invalid. Previously `verify` reported OK, `behaviour` reported *complete* (exit 0) and `replay` served files
  containing two sessions or no start, while `lab` alone refused them
- **`verify` gains a structure verdict** (`Structure: VALID | INVALID — <code> at seq N | INCOMPLETE — no_end`)
  while its default exit codes stay unchanged (0 intact / 1 failed) so existing scripts keep working;
  `--require-structure` makes a structural or completeness defect fatal (exit 3)
- **`behaviour` completeness derives from structure** — a file with no start, two starts, a relocated end or two
  identities can no longer be *complete*; `identities` is reported alongside `sessions` (which still counts
  files, deliberately unchanged in meaning), and rows carry `session_id` and `structure`
- **`inspect` publishes all four verdicts and can gate CI** — `--check` exits 3 on integrity failure, 4 on
  structure failure; an unparseable file now says why it cannot render instead of silently producing nothing
- **`inspect` causal story no longer mis-attributes calls** — the correlation map is scoped per session; two
  denied calls previously both rendered as `#14` when the first was `#6`
- **`replay` refuses structurally invalid evidence** by default (it was serving files `lab` rejected);
  `--allow-invalid-structure` is the explicit escape hatch for tamper-testing workflows. `lab` keeps
  `incomplete_recording` for open recordings and gains `structure_invalid` for structural defects, with every
  eligibility rule unchanged
- **Sequence/identity sentinel fix** — an empty or mixed identity at `seq 0` was invisible to both the chain
  check (`FirstBadSeq == 0` meant "intact") and the structure check; such a file had `lab` PASS and `replay`
  serve it. Problems are no longer encoded in a `uint64` whose zero value means "none"
- **New trust-bearing gates are mutation-tested** — removing the replay gate, the lab validity gate, the
  `inspect --check` mapping or `--require-structure` now fails the suite; previously all four could be deleted
  with `go test ./...` green

## [0.4.0] — 2026-10-01
- **Raw-protocol fail-closed (boundary invariant)** — client→server lines that fail JSON-RPC parsing were previously forwarded untouched ("protocol safety"); they are now REJECTED (JSON-RPC `-32700` parse error / `-32600` invalid request, id echoed when detectable) and recorded, and actionable methods sent as id-less notifications are refused too — a more lenient upstream parser (batch arrays, NaN literals, `jsonrpc` version drift) must never form a second interpretation of an actionable request. Protocol notifications (`notifications/*`) still pass. Invariant pinned by `TestUnparseableLinesNeverReachUpstream`; mux's silent drop got the same recorded-rejection contract
- **Causal linkage for boundary rejections** — the `malformed-request` decision now carries `parent_seq` of its `tools/call` event like any ordinary mediated call (spec §6.1), and the event carries best-effort `_meta.traceparent`
- **`RedactJSON` non-JSON fallback fix** — raw unparseable bytes were hand-quoted without escaping, so garbage containing quotes produced invalid JSON and the evidence event failed to marshal (surfaced by the fail-closed path above); the fallback now stores a properly-escaped JSON string, still redacted
- **`docs/QUICKSTART.md`** — the five-minute path (understand → install → in front of a server → record → replay) plus the second five minutes (denied call → inspect → verify → replay → what-if): the adoption proof, written as timed steps
- **P0 fix: malformed `tools/call` can no longer bypass the mediator** — an unparseable `tools/call` (bad params, missing tool name) was silently forwarded upstream (`mediatedCall` treated the parse failure as "nothing to mediate"). The boundary now rejects it (structured JSON-RPC `-32602`, rule `malformed-request`), records the rejection as evidence (`tools/call` + `policy/decision`), and never forwards it — proxy AND mux paths. Regression test proves: malformed → structured rejection → upstream receives nothing
- **`tapelog replay` verifies the chain first** — altered evidence is refused by default (it will not quietly serve a modified cassette as "the recorded session"); `--allow-unverified` is the explicit escape hatch for tamper-testing workflows, with a loud warning. `tapelog test`/`fuzz`/`whatif` keep cassette semantics by design
- **Value-flow taint degrades conservatively under unresolved sources** — a source call still in flight at the result-wait deadline is now assumed to taint matching sinks (value flows degrade to conservative semantics, never a silent pass; `Options.ResultWait` makes the deadline explicit and testable)
- **Proxy `toolIDs` leak fix** — denied requests no longer accumulate in the request-correlation map
- **"observe-only" terminology fixed (no-policy mode)** — no `--policy` means "no policy rules; boundary protections remain active" (schema firewall, limits, drift pinning, injection scan, redaction). Wording aligned across README/POLICY/CLIENT-SETUP/doctor/flag help/adapters
- **Status unification + supply-chain hardening** — `internal/buildinfo` (`0.4.0-dev`) is the single version source; README status block names released/stable/experimental explicitly; GitHub Actions pinned to immutable SHAs (with version comments), `govulncheck` + `goreleaser` pinned to exact versions; README restructured product-first (30-second demo → install → one config change → what you get → security model → docs → command tour)
- **Trajectory assertion hardening (`tapelog test`)** — count ceilings (`min_times`/`max_times` on `called`/`attempted`; `times` stays exact), toxic-flow assertions as scenario checks (`flow_denied: {from, to, times?}` — attempted AND blocked, matched via the decision's `[taint sources: …]`/`[contaminated by: …]` provenance; `flow_attempted` for any flow decision; mux namespaces match either form), and `max_depth: N` (ceiling on the trajectory's longest data-dependency chain — a call whose args carry recorded values from an earlier call's result is one hop; failures render the chain). Semantics pinned in docs/TESTING.md
- **Session schema v0 causation/correlation (spec §6.1, additive)** — optional `parent_seq` (the `seq` of the causing event: decision/result events point at their `tools/call` event; v0.6 async continuations attach without a schema break) and `traceparent` (`_meta.traceparent` passthrough recorded on call/decision/result). Old readers unaffected (fields absent by default)
- **Blob refs for large payloads (spec §6.2, additive)** — `record`/`mux --blob-threshold N` offloads oversized `args`/`result` to a content-addressed `<log>.blobs/` store (`<digest>.blob`); the event carries `{"$blob": {sha256, size}}` so the hash chain binds the digest. `verify` re-hashes blobs (digest mismatch fails; absent store warns but the chain stands); replay/`tapelog test`/`whatif`/`inspect` resolve placeholders (never display them as content) and fail loud when the store is missing — replay needs the blob store
- **Signed session checkpoints (`tapelog checkpoint` / `verify --checkpoint`)** — pins {session, seq, chain_head, created_at} with a signature (WHO: SSHSIG over an OpenSSH key, `ssh-keygen -Y verify` interoperable; or cosign keyless via `cosign sign-blob` with identity extracted from the bundle) **and a transparency-log witness (WHEN)**: a Rekor `rekord` entry over the exact statement + signature + key, verified fully offline (entry binding → signed entry timestamp → RFC 6962 inclusion proof → signed tree head) against the pinned log key. `--witness none` is an explicit opt-out that `verify` flags loudly ("signed but NOT witnessed — the 'when' is unproven"). Periodic mid-session checkpoints via `--seq N`; `verify --expect` remains the zero-dependency anchor. Live-validated against rekor.sigstore.dev (note: the deployed log requires sha512 SSHSIG hashes and serves entries at `/api/v1/log/entries`; the client probes both mounts). Docs: `docs/CHECKPOINTS.md`
- **Real-world compatibility tier wired up (`tools/compat-live.ps1` + `compat-live.yml`)** — the adaptive live tier now runs against pinned real servers: `@modelcontextprotocol/server-filesystem@2026.8.31`, `mcp-server-fetch@2026.8.18` (PyPI), `@modelcontextprotocol/server-github@2025.4.8`, `@modelcontextprotocol/server-postgres@0.6.2`, all 4 validated live locally (catalogs discovered, safe probes chosen, what-if parity 0 changed). The conversation is catalog-driven and safe-by-construction (`internal/compat/liveconv.go`): read-only probes with benign args (SELECT 1, https://example.com), rule-deny probes only for tools the policy actually blocks (denied pre-forward — nothing executes), and the value-taint flow only when both ends exist (the sink carries a value copied verbatim from the source's recorded result, so what-if parity holds by construction). Scheduled weekly CI matrix (non-blocking) with per-server evidence artifacts; `TestAdaptiveAgainstFixtureServer` proves the live-tier logic hermetically
- **DENIED provenance + causal story in `inspect`** — `inspect --plain` now renders a "denied calls" section: one block per denied call with event #, session id, tool + args, rule + reason, and provenance lines ("value from read_secrets — produced at call #1 (result #2)") plus a one-line causal story (call → deny ← source → never executed; a result recorded despite a deny is flagged `no_deny_bypassed`). Correlates tools/call ↔ policy/decision ↔ tools/result by request id and maps contamination source names back through the mux `a__` namespace; the TUI detail pane shows the same block on deny events
### v0.3 line — folded into [0.4.0]
- **MCP compatibility laboratory (`internal/compat/`)** — permanent in-repo integration matrix, one runner two tiers: hermetic era-variant scripted servers (2026-07-28 / 2025-11-25 / 2024-11-05 × stdio subprocess / streamable HTTP — 6 cells, CI-blocking) plus an opt-in real-world tier (`TAPELOG_COMPAT_LIVE=…`, scheduled, never blocks CI). Every cell drives the full loop: discover → connect → tools/list → tools/call → errors → `_meta` → MRTR → redaction → policy → recording → replay → what-if, with **server-side observations** (methods seen, per-request `_meta`, handshake notifications, MRTR `inputResponses`, HTTP request-metadata headers) as wire-level ground truth. `internal/compat/fakecmd` is the standalone hermetic server binary (also powers the canonical demo)
- **Canonical trajectory demo (`examples/trajectory-demo/`)** — one artifact showing the whole product: record → trajectory → policy block (rule deny + value-taint deny with `[contaminated by: …]` provenance) → verify + chain head → 9 trajectory assertions green → what-if 0 changed → replay → tamper → **`tapelog test` still GREEN while `verify` pins the edit** → anchored verify catches truncation. The money line: *behavioral tests pass on a tampered log; the hash chain doesn't*
- **Trajectory assertions: `attempted:` + `denied:` over the full boundary** — `denied:` previously matched only answered calls, so "the boundary denied X" was unassertable (denied calls live in `Unanswered`); verdict assertions and the new `attempted: {tool, args?, times?}` now see everything that reached the boundary, while `called:`/`never_called:`/`sequence:` stay on realized side effects
- **`resultType` on every result** — spec 2026-07-28 requires it; mux and replay now normalize harness-facing results (`jsonrpc.WithResultType`, "complete" when absent, MRTR `input_required` untouched) while the session log keeps upstream bytes verbatim
- **`policy whatif` / scenario `policy:` share one re-evaluation walk (`replay.Reevaluate`)** — flow-aware (value taint rebuilt from recorded results → session flows → per-call rules) for both tools; scenario `policy:` re-evaluation previously ignored flow rules (same gap whatif had)
- **Startup checklist** — `record`/`mux` print ✓ boundary/policy/recording/audit-mode; `mux` also reports each upstream's negotiated protocol era (modern, stateless vs legacy handshake)
- **MCP 2026-07-28 compatibility (dual-era)** — the client and both bundled servers now speak the current spec end-to-end: `server/discover` probe with legacy `initialize` fallback (handshake-less against modern servers), per-request `_meta` self-description (`io.modelcontextprotocol/{protocolVersion,clientCapabilities,clientInfo}`), `-32022 UnsupportedProtocolVersionError` negotiated retry, `MCP-Protocol-Version`/`Mcp-Method`/`Mcp-Name` request-metadata headers on streamable HTTP, and `server/discover` is protocol plumbing (never policy-mediated — a deny there would break compliant clients at discovery). The proxy's un-mediated plumbing set is now `initialize`/`ping`/`tools/list`/`server/discover`; JSON-RPC notifications are never mediated
- **MRTR params preserved end-to-end** — `tapelog mux` no longer rebuilds `{name, arguments}` for upstream calls; the client's params (`_meta`, `inputResponses`, anything future) are forwarded verbatim with only the namespaced name rewritten (`CallToolParams`), so Multi Round-Trip Requests work through the boundary
- **Audit fail-closed (`--audit-mode`)** — session-log append failures no longer vanish: the first failure latches "audit-degraded" and, in strict mode (default), the affected call is denied and every later call fails closed (`rule_id: audit-degraded`) — an unrecorded tool call must not execute. `best-effort` is the explicit availability-over-integrity opt-out. `tools/result` appends that fail block result delivery the same way
- **`tapelog verify --expect <chain_head>`** — external trust anchor for the hash chain: the chain alone anchors at an empty `prev_hash`, so a whole-log rewrite with recomputed hashes is internally consistent; `--expect` (chain head recorded outside the log — CI output, ticket) detects whole-log rewrites and tail truncation. `verify` now prints the chain head
- **`--strict-schemas`** — opt-in strict schema-firewall: tools whose advertised `inputSchema` fails to compile are denied (`schema-firewall`) instead of silently skipped (default fail-open preserved)
- **Policy evaluates redacted args** — `where:` conditions now see the same (deterministically redacted) args the log records, so `tapelog policy whatif` verdicts reproduce live verdicts exactly; value-taint matching still operates on raw args (it must, to match secret values). This resolves the cedar.go/mediator contract drift
- **`policy whatif` replays value-level taint** — the what-if walker now rebuilds contamination from recorded results and consults `flows: mode: value` rules before session flows and per-call rules (it previously ignored value-mode flows, so contaminated-sink denies looked like verdict "changes"); parity locked by `TestWhatIfReproducesValueTaintVerdicts`
- **Log-overwrite protection unified** — the "existing log will be OVERWRITTEN" warning lives in `session.NewWriter`, so `record` and `mux` behave identically (mux previously truncated silently)
- **Release workflow fixed** — `release.yml` contained two `on:`/`jobs:` blocks (the survivor-by-luck used `@latest`/`main` installs); now one job, pinned installer actions, no floating tool versions
- **Version single-sourcing** — `internal/buildinfo` is the one place the version lives (CLI, MCP clientInfo, mux/replay serverInfo); goreleaser stamps it. MCP protocol version single-sourced in `transport.MCPVersion`
- **Live-tamper-safe recording** — the session writer no longer holds the log file open: each event is appended open-write-close, so an on-camera edit of a live log can neither lock the editor out (Windows sharing) nor strand later events at a stale file offset. A mid-session edit now prints a loud WARNING, the recording keeps appending, and `tapelog verify` pinpoints the edit (regression test: `TestWriterSurvivesExternalEditMidSession`)
- **Web dashboard: live chain verdict** — `/api/log` now verifies the hash chain on every poll (not just at page load) and carries `chain_ok`/`first_bad_seq`/`problem` + the file's max seq, so a tamper mid-session surfaces the ⚠ banner live in both the record dashboard (`--approval-listen`) and `tapelog web --dir` — plain refresh (F5) shows the truth; the renderer also re-pulls the whole log when the chain breaks or the file is replaced/truncated, instead of keeping stale pre-tamper rows
- **Windows + Roo/Cline integration fix (docs)** — root-caused "MCP error -32000: Connection closed": Roo wraps stdio commands in `cmd.exe /c`, whose quote rule chops paths containing spaces at the first space; fix is 8.3 short paths in the client config (recipe in `docs/CLIENT-SETUP.md`), verified byte-for-byte against Roo 3.54.0's actual spawn code
- **Onboarding: `docs/CLIENT-SETUP.md`** — "put it in front of your agent" recipes for Roo Code, VS Code (Copilot), Claude Code/Desktop, and any stdio client; documents the client-driven `confirm` trap (stdin is the MCP channel → approval queue / `--auto-confirm` / deny-only) and a troubleshooting table. README gains an Install section (releases + go install) and a 2-minute "Connect your agent" quickstart
- **Web dashboard: chain badges** — `tapelog web --dir` now verifies every session's hash chain on listing: tampered logs get a `⚠` in the session picker and a red "chain broken — first bad event: seq N" banner instead of silently rendering as authentic (spotted during product testing)
- **Spec community materials** — `spec/OTel-COMPARE.md` (honest "why not just OpenTelemetry" comparison + bridging strategy via decision spans) and `spec/FAQ.md` (adoption/security/process, including the hash-chain non-claims); linked from `spec/README.md`. Community distribution happens with the Show HN launch
- **`tapelog test` CI integrations** — `--junit <path>` JUnit XML report (GitLab/Jenkins/Azure/GitHub ingestion), `--annotate` GitHub Actions `::error` inline annotations (automatic on `GITHUB_ACTIONS`), and a CI dogfood job running tapelog's own scenario suite with the JUnit artifact uploaded
- **Fuzz operators v2** — 5 new boundary probes: `arg_unicode` (zero-width/homoglyph value evasions), `arg_boundary` (empty/null/extreme values), `arg_encoding` (base64/URL-encoded), `tool_namespace` (`server__tool` confusion), `swap_rotate` (non-adjacent reordering — sink moved before its source, the case `swap_adjacent` cannot reach); 13 operators total, all on the deny→allow oracle
- **Value-level taint (`flows: mode: value`)** — CaMeL-inspired precision layer (experimental, ADR 0005): flow rules fire only when sink arguments are *contaminated* by recorded values from source-tool results (`[contaminated by: ...]` evidence in deny reasons); clean sinks pass. Bounded contamination store; honest heuristic limits documented (transformations evade matching — keep `mode: session` for must-never flows)

## [Unreleased]

### Added
- **Release automation** — `release` workflow (tag `v*` or workflow_dispatch):
  goreleaser builds 6 static binaries, per-archive SBOMs (syft), keyless
  cosign-signed `checksums.txt`, and GitHub build-provenance attestation —
  fulfilling the SECURITY.md supply-chain promise; `workflow_dispatch`
  backfills older tags

## [0.2.0] — 2026-09-30

The **agent regression CI & boundary hardening** release: behavioral
testing at the tool boundary (`tapelog test`), policy fuzzing (`tapelog fuzz`),
batteries-included policy packs, a multi-session web dashboard, setup
preflight (`tapelog doctor`), and OTel policy-decision events. Built from
the community research in docs/RESEARCH-EVOLUTION.md — the tools the
eval/gateway wave does not have.

### Added (agent regression CI & boundary hardening, per docs/RESEARCH-EVOLUTION.md)
- **Multi-session dashboard** — `tapelog web --dir sessions/` serves a read-only browser dashboard over a directory of session logs: session list (event + denial counts), per-session event timeline, same security posture (loopback pinning, strict CSP, optional token, path-traversal-proof file selection)
- **`tapelog doctor`** — setup preflight: policy compiles (rules/Cedar/limits), WASM plugins load, platform capabilities (Landlock, terminal), log writability, node presence; ✓/!/✗ report, CI-friendly exit codes
- **OTel decision spans** — `policy.decision` is now a first-class span event on every `execute_tool` span (verdict, rule_id, reason as both attributes and event fields): the boundary's decisions are queryable in any trace backend
- **Policy packs** (`packs/`) — batteries-included policies for filesystem/git/fetch/postgres/slack/shell + a default-deny starter: deny the dangerous, confirm the risky, allow the routine. Every pack is Cedar-compiled in CI (`packs/packs_test.go`); live dogfooding caught and fixed an AND-vs-OR `where` authoring bug (now documented in POLICY.md)
- **`tapelog fuzz`** — the boundary hardening lab: attack-shaped mutations of recorded sessions (tool-name case/space/homoglyph/traversal spoofing, argument traversal/type/overflow tampering, toxic-flow reordering) re-evaluated against the policy; a deny→allow flip is a reproducible boundary bypass with rule id + ATLAS tactic hint. CI exit codes + JSON output. (Research: nobody fuzzes the agent↔tool boundary — prompt fuzzers and ATLAS generators exist, policy-boundary fuzzing did not.)
- **`tapelog test`** — behavioral regression at the tool boundary: YAML scenarios assert over recorded cassettes (`called`/`never_called`/`args`/`times`/`sequence`/`taint_never`/`result_contains`/`allowed`/`denied`/invariants), optional policy re-evaluation (what-if semantics), CI-native exit codes + `--plain`. Research note: the eval wave does prompt scoring; nobody does tool-call trajectory regression with hash-chained provenance

## [0.1.0] — 2026-09-30

First release: the flight recorder, boundary, and replay engine for MCP
tool-calling agents. Highlights: tamper-evident hash-chained session logs
(with a standalone format spec + conformance vectors), VCR-style hermetic
replay + diff, the full decision pipeline (policy rules with Cedar
conditions, schema firewall, limits, taint flows, drift detection,
injection scanning), human-in-the-loop approvals (terminal, queue, web
dashboard), multi-server mux, WASM plugins, OTel export, and an OS
sandbox for spawned servers.

### Research-driven gap fills (2026-09-29 internet research)
- **mux catalog aggregation + precise routing** — `resources/list`, `prompts/list`, `resources/templates/list` merge across upstreams (annotated `_tapelog_server`); prompt names namespaced like tools; resource reads route to the catalog's owner (unknown URIs fall back to first-success) — closes the last research item
- **Review web dashboard** — `--approval-listen` now serves a dependency-free dashboard on the same socket: parked approvals (with notes) + live session-log tail. XSS-safe rendering (`textContent` only + strict CSP), DNS-rebinding Host pinning, CSRF same-origin checks, `no-store`; `tapelog queue` CLI unchanged
- **Remote approval queue ("quarantine queue")** — `--approval-listen` parks `confirm` verdicts at the boundary (held, then fail-closed to deny on `--approval-timeout`); `tapelog queue list|allow|allow_session|deny` decides from any terminal via a localhost JSON API (`--approval-token` optional); every decision is recorded with its provenance and note
- **Injection scanning of results** — tool/surface results are scanned for prompt-injection markers (conservative built-ins + `injection.patterns`); `injection.mode: log` (default) records explainable `injection-scan` decisions, `confirm` routes through the HITL prompt, `deny` withholds the payload via the result-veto (original still recorded)
- **No unmediated surface** — `resources/read`, `prompts/get`, `resources/templates/get` (and any future method) now flow through the full decision pipeline as "surface calls" (`tool:` = method name, args = MCP params; Cedar `where:` conditions can match `context.args.uri`/`name`). They consume limits, feed taint, and are recorded as `tool_call`/`tool_result` events. In `mux` they route first-success in deterministic server order. Previously these crossed the boundary completely uninspected.
- **Schema firewall (inbound)** — tool-call arguments are validated against the `inputSchema` the server itself advertised (jsonschema draft-agnostic); violations deny with `rule_id: schema-firewall` before anything is forwarded. Fail-open for tools without/broken schemas. ("Allowing a tool name isn't enough — risk hides in the payload.")
- **Session limits** — policy `limits:` block: `max_calls` (session budget), `max_calls_per_tool` (glob patterns), `max_per_minute` (sliding window), `max_response_bytes` (payload cap — over-cap results are replaced with a structured `response_too_large` error while the log keeps the evidence). Explainable denials like every other verdict.
- **`tapelog completion`** — bash/zsh/fish/powershell scripts.

### Project identity
- **Renamed `cassette` → `tapelog`** after a full availability audit (docs/launch/NAMING.md): npm/PyPI/crates all clear, GitHub org `tapelog-dev`, Go module `github.com/Caseymccallum/tapelog`. The replay type is now `replay.Tape`; logs are "tapelogs". Also fixes a long-standing `.gitignore` bug (bare `cassette` pattern had silently excluded `cmd/` from git since the first commit)

### Post-plan development — v2, part 5 (roadmap complete)
- **OS sandbox for spawned servers** — `--sandbox-ro`/`--sandbox-rw` (repeatable) run MCP servers under Landlock filesystem restrictions via a hidden re-exec wrapper (`__sandbox_exec` + `syscall.Exec`); strict by default, `--sandbox-lenient` degrades with a warning; baselines keep binaries runnable (`/usr`, `/lib`, … RO, `/tmp` RW). Linux enforcement + gated kernel test; cross-platform wrapping tested everywhere

### Post-plan development — v2, part 4
- **WASM plugin API** (`--plugin`, repeatable) — sandboxed pure-computation plugins (wazero, no cgo): `verdict_hook` may only **tighten** decisions (host-enforced lattice — plugins physically cannot weaken enforcement; failures fail closed) and `redact_hook` adds org-specific redaction on already-redacted text. WASI reactor ABI documented in `docs/PLUGINS.md` with a complete reference plugin (`examples/plugins/argguard`, built with plain Go)

### Post-plan development — v2, part 3
- **Session log format as standalone spec** — `spec/` package: normative `session-log-v0.md` (RFC 2119, byte-precise canonical form, verification algorithm, replay semantics, conformance classes), JSON Schema, **machine-readable conformance test vectors** (`tools/gen-vectors` + `internal/session` conformance tests bind spec ↔ code). `docs/SCHEMA.md` is now a pointer stub

### Post-plan development — v2, part 2
- **`tapelog mux`** — one mediated boundary across multiple MCP servers: stdio commands and/or streamable-HTTP endpoints from a YAML config; tools namespaced `<server>__<tool>`; one session log, one policy, one taint state. Live listing refresh keeps `--deny-on-drift` working across upstreams
- **`internal/mediator`** — the decision pipeline (drift → flows → policy → confirm → taint → logging) extracted into one shared package used by `record` and `mux`; enforcement semantics cannot diverge
- **Transports** — `internal/transport`: stdio subprocess + minimal streamable-HTTP client (POST, JSON/SSE responses); `internal/mcpclient`: minimal MCP client (initialize / tools/list / tools/call) with id correlation

### Post-plan development — v2, part 1
- **Flow rules (toxic-flow guards)** — cross-tool data-flow policy: `flows:` in the policy file restricts `from` (source) → `to` (sink) tool pairs with session-scoped taint. Deny reasons name the taint sources. `action: deny` or `confirm`. This closes the category-wide gap (including the enterprise platforms') that per-call rules cannot see. Sequence-aware in `policy whatif` too
- **Human-in-the-loop `confirm` UX** — git-style terminal prompt: allow once / allow for session / deny. Fail-closed without a terminal unless `--auto-confirm`; every treatment recorded

### Week 4 — polish & launch
- **`tapelog inspect`** — interactive TUI session viewer (bubbletea): event timeline with color-coded verdicts, drift flags, payload detail pane; `--plain` for CI
- **`tapelog export otel`** — OpenTelemetry trace export (GenAI semantic conventions: `execute_tool` spans, `gen_ai.tool.name`); OTLP/HTTP to a collector or stdout
- **TypeScript & Python adapters** — thin, dependency-free config wrappers (`adapters/`)
- **Rogue-agent demo** — scripted 60-second attack story (`examples/rogue-agent/`)
- **Show HN launch kit** — post draft, checklist, talking points (`docs/launch/`)
- Replay: tool catalogs now merge listings newest-wins (no duplicate descriptors)

### Week 3 — policy v1
- **Cedar condition engine** — `where` clauses are real Cedar expressions over `context.tool` / `context.args`, evaluated by `cedar-go` (fail-closed on errors); full reference in `docs/POLICY.md`
- **Scoped grants** — `expires` (RFC 3339 time-boxed authority) + `tasks` (per-task delegation via `record --task`)
- **`tapelog policy whatif`** — re-evaluate a recorded session against a candidate policy and diff verdicts; exits non-zero on change → CI policy regression tests for agents
- **`tapelog policy compile`** — export policies as portable Cedar text (documented approximations)
- **`record --deny-on-drift`** — deny tool calls whose descriptor changed since first listing (tool-poisoning defense); tool calls now wait for in-flight `tools/list` responses so enforcement cannot be raced; descriptor pins now recorded on every call

### Week 2 — replay engine
- **`tapelog replay`** — deterministic re-execution: a recorded session is served as a **hermetic MCP server** (`initialize`, `tools/list`, `tools/call` answered from the recording). VCR semantics: redaction-aware canonical matching (`--match exact|subset|tool`), consume-once FIFO, **fail-loud** on missing recordings (error `-32011` + remediation hint), `--strict` mode for CI
- **`tapelog diff`** — compare two sessions (added/removed calls, changed results, order drift); exits non-zero when they differ
- `tools/list` events now recorded (descriptors redacted + canonicalized) — replay can serve the exact tool catalog
- `tool_descriptor_hash` is now over the *redacted canonical* descriptor (verifiable from the log)

### Week 1 — foundation
- **Session log format v0** — append-only, hash-chained JSONL (`docs/SCHEMA.md` at the time — now `spec/session-log-v0.md`)
- `tapelog record` — transparent stdio MCP proxy that records every tool call, result, and policy verdict into the session log (with secret redaction and tool-descriptor hash pinning)
- `tapelog verify` — hash-chain verification; detects modification, deletion, and reordering (including recomputed-hash edits of individual events; whole-log rewrites require an external anchor such as the chain head printed at session end)
- `tapelog policy test` — evaluate sample tool calls against a policy before deploying it
- **Policy engine v0** — ordered YAML rules (`allow` / `deny` / `confirm`), glob tool matching, explainable decisions with reasons, fail-closed default
- Threat model, governance set (GOVERNANCE, SECURITY, CODE_OF_CONDUCT, CONTRIBUTING), ADRs 0001–0003
- CI (Linux race tests + Windows), goreleaser config

### Notes
- Working name `tapelog`; final name TBD before publication
