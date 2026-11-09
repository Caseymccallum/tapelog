# Conversation attribution with a real client (experimental)

> **Not part of the installation path.** Nothing on this page is needed to record, verify, replay or
> test a session. It documents one controlled experiment, the evidence it produced, and the limits of
> what that evidence means.

## Two different claims, which must not be merged

1. **Tapelog can ingest and analyse client-declared conversation metadata.** This is a product
   capability: requests carrying `_meta` keys (`io.tapelog/conversationId`, alias
   `io.hermes.agent/conversationId`) are recorded with `conversation_id`, grouped in `behaviour`, and
   can scope a regression case. It works with **any** client that sends those keys — including the
   fictional driver in [`examples/regression-demo/`](../examples/regression-demo/README.md), which
   needs no Hermes at all.

2. **A particular client actually supplied that metadata.** That is a property of the client, not of
   Tapelog. The live evidence below came from a Hermes gateway running a **temporary client-side
   patch**. An unmodified Hermes installation does not send these keys as of the tested version, so
   its recordings show no declared conversations — which is correct, and is not a Tapelog defect.

Everything on this page is evidence for (1). It is evidence for (2) only for the specific patched
build described here.

## The artifact

Two snapshots of one real session, recorded through Tapelog by a Hermes gateway:

| File | Events | Chain head | Structure |
|---|---|---|---|
| `test-fixtures/m14.1-gateway/session-gateway-open.jsonl` | 16 | `4a87cd54…` | **INCOMPLETE — no_end** |
| `test-fixtures/m14.1-gateway/session-gateway-closed.jsonl` | 17 | `ef6befe4…` | VALID, complete |

**These two files are derived, not the original bytes.** One machine-specific value
(`session/start.policy_id`, the recorder's absolute policy path) was replaced with the neutral name
`policy.yaml`; everything else is field-identical, and both files were re-chained because changing a
payload invalidates a chain. The keys, along with the originals' heads
(`7eda1b89…` open, `da4d0c04…` closed), are recorded in
[`DERIVED.md`](../test-fixtures/m14.1-gateway/DERIVED.md) and asserted by the test below, so the
artefact and its provenance cannot drift apart. The immutable originals stay outside the public
tree; ask the maintainer if you need them for a review.

The open file reproduces **the snapshot that was verified during the M14.1 run**, event for event:
the gateway wrote `session/end` eleven minutes later, when its child process terminated, so the
closed file is that same session plus one event. The open file remains a byte prefix of the closed
one, and the pair is kept because the difference between "still open" and "closed" is exactly what
people get wrong about recordings.

What the session contains, asserted by `TestGatewayEvidenceIsVerifiableAndHonest`:

- one `session/start`, first; one `session/end`, last (in the closed file);
- **5 mediated calls**, 5 policy decisions (**4 allow, 1 deny** — the deny is rule `no-delete`), 4
  recorded results (one of them an error from the fixture's `boom` tool);
- **2 client-declared conversations**: three calls in one (`local-399e…`, two `read_file` plus
  `boom`), two in the other (`local-f6a2…`, `read_file` and the denied `delete_file`);
- **zero unattributed calls**.

Check it yourself:

```bash
go test ./internal/session -run TestGatewayEvidence -v
tapelog verify test-fixtures/m14.1-gateway/session-gateway-open.jsonl     # chain intact, INCOMPLETE — no_end
tapelog verify --require-structure test-fixtures/m14.1-gateway/session-gateway-open.jsonl   # exits 3
tapelog inspect test-fixtures/m14.1-gateway/session-gateway-closed.jsonl --plain
```

Analyse one file, not the directory — the two snapshots are the same session and a directory run
counts both:

```bash
mkdir -p /tmp/m141 && cp test-fixtures/m14.1-gateway/session-gateway-closed.jsonl /tmp/m141/
tapelog behaviour conversations --dir /tmp/m141
# Behaviour conversations · complete
# Conversations 2 · sessions 1 · calls 5 · results 4 · tool errors 1 · denied calls 1 · deny decisions 1 · unanswered 1
```

## What this proves — and what it does not

**Proved:** a real agent harness held one MCP connection across two conversations; Tapelog recorded
both, attributed every call, and every surface agrees on the facts. The chain is intact and the
structure is valid. Conversation-scoped analysis is possible on genuine client traffic, not just on
fixtures.

**Not proved, and not implied:**

- **Not that the session was complete when reported.** The snapshot verified during M14.1 was
  INCOMPLETE. It only became complete eleven minutes later. An open session with an intact chain is
  not evidence of a complete session — that is why both snapshots are committed here.
- **Not that the ids are identities.** They are client-declared strings. They are not authenticated,
  they cannot create or close a session, and they cannot repair a mixed-identity file
  (`TestGatewayEvidenceCannotRepairStructure`).
- **Not that an unmodified Hermes (or any other client) sends them.** The experiment required a
  temporary patch to the gateway's Python modules. Without it, `conversation_id` is simply absent,
  and a lab case that scopes to a conversation returns `conversation_absent`.
- **Not a supported integration.** No client-side patch ships with Tapelog, and none is planned here.

## Why unmodified clients show nothing

MCP has no standard "conversation" concept in the eras Tapelog supports. The metadata travels as
request `_meta` keys, which a client must choose to send. Tapelog's boundary records what it receives
and never invents a group: absent attribution is reported as absent.

## If you want to attempt the patched-client experiment

It is a controlled experiment on someone's working installation, not an install step. Read
[`M14_HERMES_GATEWAY_PLAN.md`](reviews/TAPELOG_M14_HERMES_GATEWAY_PLAN.md) and the runbook it
describes before doing anything, and expect this shape:

1. Back up every file you will touch and record its hash; verify the backup byte-for-byte.
2. Patch the client off-tree first and rehearse, so nothing is modified in place.
3. Add the boundary to the client's config as a **temporary** entry, with a deny-only policy — never
   `confirm` rules, which hang without a terminal.
4. Restart the client **from outside it**; the process cannot restart itself mid-session.
5. Run two real conversations, then verify from the recording, not from the transcript.
6. Remove the entry, restore both files from backup, `cmp` each one, and confirm the config hash is
   back at its pre-experiment value.

The M14.1 run took that procedure end to end and restored the installation; the same page's review
notes which markers prove the patch and the config are actually in effect.

## Status

Experimental. No supported client integration, no default configuration change, and no claim that any
harness supplies conversation metadata out of the box. If a client does send `_meta`, Tapelog will
use it — and will still call it a claim, not an identity.
