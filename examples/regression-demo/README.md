# Regression demo — what the evidence can and cannot prove

A five-minute, dependency-free demonstration of Tapelog's Regression Lab on **fictional
recordings**. It produces all three verdicts — PASS, FAIL and INCONCLUSIVE — from evidence that
already ships in this directory, so the outcomes are deterministic and nothing has to be recorded
first. Hermes is not involved and not required.

The point of the demonstration is not that Tapelog can say PASS. It is that it distinguishes
**"the evidence proves this"** from **"the evidence proves this is false"** from **"the evidence
cannot decide"**, and shows you which one you are looking at and why.

## Run it

From the repository root, with a built binary:

```console
go build -o bin/tapelog ./cmd/tapelog          # or use a released binary
./bin/tapelog lab suite --dir examples/regression-demo --suite demo-suite.json
```

Expected output (case order is the suite's order):

```text
FAIL · Tapelog replay and regression demo (fictional): 2 pass, 1 fail, 2 inconclusive
  PASS · A permitted read reached the boundary · session-two-conversations.jsonl
    http://127.0.0.1:8923/#session=session-two-conversations.jsonl
  FAIL · The recording proves read_file was NOT denied · session-two-conversations.jsonl
    http://127.0.0.1:8923/#session=session-two-conversations.jsonl
  INCONCLUSIVE · An interrupted recording cannot decide the claim · session-interrupted.jsonl
    http://127.0.0.1:8923/#session=session-interrupted.jsonl
  PASS · Scoped to one conversation: write_file was attempted there · session-two-conversations.jsonl
    http://127.0.0.1:8923/#session=session-two-conversations.jsonl
  INCONCLUSIVE · A conversation that never appears cannot be declared quiet · session-two-conversations.jsonl
    http://127.0.0.1:8923/#session=session-two-conversations.jsonl
error: one or more regression expectations failed
```

The suite's overall status is **FAIL** — `tapelog lab suite` exits `1`. That is the demo working,
not failing: one case asserts something the evidence contradicts, and a suite reports the worst
verdict it found. Exit codes are `0` PASS, `1` FAIL, `2` INCONCLUSIVE.

Run one case on its own to see the supporting detail:

```console
./bin/tapelog lab run --dir examples/regression-demo --case 03-interrupted-inconclusive.json
./bin/tapelog lab run --dir examples/regression-demo --case 03-interrupted-inconclusive.json --json
```

## What each case is doing

| Case | Evidence | Expected | Why |
|---|---|---|---|
| `01-allowed-read.json` | complete session | **PASS** | A call to `read_file` crossed the boundary. The claim is that an attempt occurred — not that its result was correct. |
| `02-denied-read-fails.json` | the same complete session | **FAIL** | The evidence is complete and unambiguous, and it establishes the opposite: `read_file` was allowed, never denied. A confident negative is a real verdict, not a bad recording. |
| `03-interrupted-inconclusive.json` | `session-interrupted.jsonl` | **INCONCLUSIVE** (`incomplete_recording`) | Chain intact, but the session never ended. Tapelog will not upgrade an interrupted recording to a verdict. |
| `04-scoped-conversation.json` | complete session, scoped to `demo-beta` | **PASS** | One MCP session carries two client-declared conversations; the scope narrows the evidence before anything is decided. |
| `05-absent-conversation.json` | complete session, scoped to `demo-never-appeared` | **INCONCLUSIVE** (`conversation_absent`) | An absent conversation is not an observed quiet one, so a scoped "no attempts" claim can never PASS on evidence that never mentions it. |

Compare the two INCONCLUSIVE cases with the FAIL: they are different answers. `03` says *"the
evidence is insufficient"*; `02` says *"the evidence proves you wrong"*. A tool that collapsed
those into one verdict would be unusable for release gating.

## The recordings

Both recordings are fictional and were produced through Tapelog's own boundary
(`tapelog record` + the hermetic `fakecmd` fixture). They contain no real paths, no credentials
and no personal data.

- `session-two-conversations.jsonl` — one MCP session, two client-declared conversations
  (`demo-alpha` calls `read_file`, `demo-beta` calls `write_file`), complete.
- `session-interrupted.jsonl` — the same session with its `session/end` never written, which is
  what an interrupted recorder leaves behind. Chain intact, structure `INCOMPLETE — no_end`.

Confirm both properties yourself before trusting the cases:

```console
./bin/tapelog verify examples/regression-demo/session-two-conversations.jsonl   # VALID, closed
./bin/tapelog verify examples/regression-demo/session-interrupted.jsonl         # chain intact, INCOMPLETE — no_end
./bin/tapelog inspect examples/regression-demo/session-interrupted.jsonl --plain
```

## Regenerating the recordings

`generate.sh` rebuilds both files from scratch through the boundary. It needs Go, nothing else:

```console
bash examples/regression-demo/generate.sh
```

The regenerated files have new timestamps, session ids and hash heads, so they differ byte-for-byte
from the committed ones; the **verdicts** must be identical. The script asserts the properties the
cases depend on — a valid complete session with two attributed conversations, and a file whose
chain is intact but which has no `session/end` — and refuses to finish quietly if either property
is missing.

## What this demo does not show

- **Conversation ids are client-declared claims**, not authenticated identities. Nothing here makes
  them an authorisation principal.
- **A clean recording is not evidence of clean behaviour.** Only traffic crossing Tapelog's boundary
  is recorded; a harness's built-in tools never appear, so no case can assert anything about them.
- **Replay is a separate surface** with its own gate: `tapelog replay` refuses structurally invalid,
  unverified or ambiguously paired recordings unless you pass an explicit, visible override. See
  `docs/releases/M15_REPLAY_REGRESSION_ACCEPTANCE.md`.
