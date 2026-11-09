# Tapelog — local product review

This workspace uses **fictional recordings only**. It is a local product review, not a live connection to an agent or the Hermes gateway.

## Open, stop and restart

The local review folder is `$HOME/hermes-missions/PRISM/completion` on Windows. Open `running-review.json` there for the verified URL, process ID and source commit of the current run. `local-review.json` identifies the reviewed executable, its SHA-256 and the fictional library.

From PowerShell:

```powershell
$review = Join-Path $HOME 'hermes-missions/PRISM/completion'
# Restart after stopping; an occupied port is refused, never taken over.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $review 'Start-TapelogReview.ps1')
# Stop only the verified review process.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $review 'Stop-TapelogReview.ps1')
```

The launcher checks the executable hash and stamped source commit, then verifies its own loopback listener and API before recording the URL. It does not change gateway settings or start real MCP traffic. The application is not installed as a Windows service and does not automatically restart after a reboot.

## Three places to start

1. **Sessions:** judge whether you can scan the recording library without opening everything. Compare the complete, incomplete and intentionally invalid fictional recordings. An intact hash chain does not mean a recording is structurally valid or complete.
2. **Open a recording → Activity:** inspect the timeline, a denied call and a tool error. Open **Recording integrity & scope**, then select a call to examine the recorded inputs, results and decision history. Try **All events**, paging and outcome/search filters. Warnings must remain legible, not disappear behind a polished summary.
3. **Behaviour → Conversations:** open an attributed conversation, follow its calls, inspect repeated activity and compare two declared conversations. Also inspect unattributed activity and the ambiguous-correlation scenario. Conversation IDs are client-declared labels, not authenticated identities or recording boundaries.

## Walk through the rest

- **Compare:** select two recordings, run comparison, and inspect both call/decision differences and behaviour changes. Differences describe recorded evidence; they do not decide whether a change is acceptable. An ambiguity warning must not be interpreted as a guessed match.
- **Regression cases:** open and run the supplied PASS, FAIL and INCONCLUSIVE examples. Read the reason for each verdict. The intentional FAIL demonstrates a contradicted expectation; INCONCLUSIVE means the evidence cannot decide, not that it passed. The fictional suite intentionally contains all three outcomes.
- **Create case:** select a recorded call or open the recording's case-authoring tab, choose an expectation and preview it. Saving is explicit and must not overwrite a recording. Cases and suites are separate from original evidence.
- **Policies:** inspect the fictional policy, rule explanations and simulation controls. Simulation is a what-if analysis, not policy activation or proof that a real tool executed. Directory review mode does not alter an active gateway policy.
- **Investigations:** save a bookmark, move elsewhere and reopen it. Bookmarks live in this browser and this recording-library context; they are not portable shared investigations.
- **Replay & CLI:** open the recording's command guidance. Replay is a hermetic command-line surface, not a browser button that executes live tools. Unverified, structurally invalid or ambiguously paired evidence has separate refusal/override rules.
- **Live approvals:** directory review has no live approval queue. An unavailable/empty state is expected; do not treat it as evidence that a live confirm/deny integration has been exercised.
- Try the appearance switch, keyboard-only navigation and a narrow window. Readable timelines and warnings matter more than decorative effects.

## Fictional data and reset

The active fictional library is `review-environment/library` under the review folder. It contains seven recordings, nine regression cases and two suites. Start with **Fictional Atlas** (repeated reads, a policy denial and a tool error), **Fictional Borealis** (conversation comparison), then the clearly labelled interrupted, invalid and ambiguous examples. The full fictional suite deliberately reports **4 PASS, 1 FAIL and 4 INCONCLUSIVE**; that FAIL is an expected, contradicted claim.

Stop the application, then restore the known library files and verify them:

```powershell
python (Join-Path $review 'review-environment/reset.py')
python (Join-Path $review 'review-environment/verify_environment.py') --binary ((Get-Content (Join-Path $review 'local-review.json') -Raw | ConvertFrom-Json).executable)
```

`review-environment/README.md` documents each scenario and its provenance. Reset restores hashed seed copies, preserves unknown files, and refuses destinations outside this exact disposable library. Use `reset.py`, not an older experiment's reset script. Browser-saved investigations are separate browser state and are not removed by recording reset.

## Important privacy and evidence limits

- Tapelog observes MCP-mediated traffic crossing its boundary. Built-in agent operations are outside its view; a clean recording cannot establish that all agent behaviour was safe.
- Cryptographic integrity, structural validity, completeness and eligibility for a particular regression assertion are different questions.
- Redaction is not a guarantee that recordings are safe to share. Argument-key masking and credential-pattern masking do not cover every secret embedded in free-form result text. Treat real recordings as sensitive material.
- This review is intentionally fictional. Do not add confidential user/gateway recordings, tokens, genuine client documents or credentials.
- Automated accessibility and keyboard checks are not screen-reader certification or a claim of complete WCAG conformance. See the accompanying acceptance report for exactly what was exercised.

## Useful feedback

Describe the screen, what you were trying to answer, and where the interface slowed you down. The most useful feedback is whether recording status is clear, the investigation flow feels coherent, and you can distinguish an observed fact from a warning, a comparison or a regression verdict. Screenshots plus the selected recording/scenario name help reproduce problems; do not include real confidential evidence.
