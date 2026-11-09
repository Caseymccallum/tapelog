# Conversation attribution

One MCP connection can serve many agent conversations. Hermes' gateway, for example, keeps a single MCP
child process alive across chats, so a boundary recording that spans a gateway lifetime contains several
conversations and no natural end.

Tapelog does **not** solve that by inventing session boundaries. It records a **client-declared correlation
identifier** instead, so activity can be grouped without any claim that a conversation started, finished, or
belongs to a person.

## Declaring it

Put the identifiers in the request's `_meta` — the same object that already carries `traceparent`:

```json
{"jsonrpc":"2.0","id":7,"method":"tools/call",
 "params":{"name":"read_file","arguments":{"path":"/tmp/x"},
           "_meta":{"io.tapelog/conversationId":"conv-3f9a",
                    "io.tapelog/toolCallId":"call-17",
                    "io.tapelog/subagentId":"worker-b"}}}
```

| Key | Alias accepted | Meaning |
|---|---|---|
| `io.tapelog/conversationId` | `io.hermes.agent/conversationId` | which conversation issued this call |
| `io.tapelog/toolCallId` | `io.hermes.agent/toolCallId` | which individual invocation this is |
| `io.tapelog/subagentId` | `io.hermes.agent/subagentId` | which delegated child issued it (optional) |

The canonical key wins if a request carries both forms. Values must be 1–128 bytes of
`[A-Za-z0-9._:-]`. Anything else is dropped and **not recorded** — a truncated identifier would be a
different identifier — and the boundary prints one warning per recording on stderr.

## Reading it

```
$ tapelog inspect session.jsonl --plain
>   2  tools/call      read_file {"path":"/tmp/a"} [conversation conv-1]
>   6  tools/call      read_file {"path":"/tmp/b"} [conversation conv-2]
>   9  tools/call      read_file {"path":"/tmp/c"} [conversation conv-1]
>  12  tools/call      read_file {"path":"/tmp/d"}
```

The fourth call declared nothing, so it is shown with no conversation. Tapelog never invents one, never
substitutes a default, and never groups unattributed calls together as though a conversation had been
declared.

In the JSON and web surfaces the fields are `conversation_id`, `tool_call_id` and `subagent_id`, present only
when the client declared them (additive `omitempty` payload fields, spec §6.3).

## What it is not

- **Not a session boundary.** `session_id` is still minted by the recorder and still covers exactly one log.
  A conversation change never creates a `session/start`, a `session/end`, or a new file.
- **Not completeness.** There is no `conversation/end` event. A conversation is never reported as finished,
  and an open recording stays *incomplete* however well attributed it is.
- **Not identity.** Attribution is a claim by the client. Nothing authenticates it, and it says nothing about
  which human was at the keyboard.
- **Not eligibility.** `verify`, structural validity and Regression Lab eligibility are unchanged. A mixed
  session stays structurally invalid even if every event carries a distinct conversation id.

## When the request id is ambiguous

JSON-RPC ids are only reliable while unique among *outstanding* requests. If a client starts a second call
under an id whose previous call has not been answered, the responses cannot be told apart — the id is the only
token on the wire, and it is now shared.

Tapelog records that honestly rather than guessing:

- each such result carries `attribution_ambiguous: true` and **no** `parent_seq`, `conversation_id`,
  `tool_call_id`, `subagent_id` or `traceparent`;
- the flag is sticky for that id for the rest of the session, because the first response may have belonged to
  either call;
- value taint is applied as the **union** of every tool that shared the id — it can over-taint (blocking a
  clean call), never under-taint (missing a real source);
- `replay` refuses such a recording and names the ids; `--allow-ambiguous-ids` serves it anyway for forensic
  or tamper-testing work.

Sequential reuse — the first call answered before the second begins — is unambiguous, is not flagged, and is
served normally.

## Privacy

Tapelog consumes attribution at the boundary but does **not** strip it from the forwarded request: the
boundary is a transparent proxy, and silently editing a client's request would be a new way to break a
server. A third-party MCP server therefore receives whatever the client chose to send.

Practical consequence: send only an opaque token. Do not put prompts, file paths, user names or anything
else meaningful into these fields. The length and charset rules enforce the *shape*; the *content* is the
client's responsibility.

No pseudonymisation is applied by default. The identifier arrives already opaque from the harness, and hashing
it would break the join against harness-side diagnostics without protecting anything the bounds do not
already protect.

## Investigating one conversation (M14)

`tapelog behaviour conversation --dir <dir> --conversation <id>` and `GET /api/behaviour/conversation?conversation=<id>`
answer "what happened in this conversation" from the recording: counted facts, the evidence limits that apply, and a
paged timeline that reaches past the overview's 200-entry cap. `tapelog behaviour compare-conversations --a <id> --b <id>`
and `GET /api/behaviour/compare-conversations?a=<id>&b=<id>` compare two conversations on counted facts only.

Neither surface treats the unattributed remainder as a conversation. Every entry point validates the id first, so the
reserved `(unattributed)` label — and any value that is not a valid identifier — is a **400 with the identifier rules**,
not a group: there is no way to ask for the remainder as though it were a conversation. Passing that validation, an
absent id returns `found: false` with the reason instead of an empty conversation, and a comparison that cannot be made
(a missing conversation, a conversation compared with itself, or any group that is not an attributable conversation)
returns `comparable: false` with an `explanation` rather than numbers. The `explanation` field is deliberately not
named `reason`: the CLI's payload minimiser strips `reason` everywhere, because a recorded policy reason can carry
sensitive provenance, and a comparison explanation must reach the terminal too. The workspace deep link is `#behaviour&conversation=<id>` (optionally `&compare=<id>`): it carries the ids
only, never arguments, results or tokens, and it inherits the same recording-level access as any other view — a link
cannot read a recording the library view would not show.

### Scoping a regression case

A lab case may name a conversation (`"conversation": "<id>"`, or the case builder's conversation field):

- **A scope is applied before any claim is decided**, by filtering the recording to the calls that declared that id. It
  can only remove evidence, never manufacture a witness: a witness from another conversation cannot satisfy a scoped
  claim.
- **An absent conversation is not an empty one.** A scope no recorded call declares makes every expectation
  INCONCLUSIVE (`conversation_absent`). A missing conversation cannot satisfy "there were no attempts".
- **Absence-style claims need the whole conversation to be observable.** If the recording holds calls with no declared
  id, or calls whose result owner is unknown, they could belong to the scoped conversation, so `no_attempts`,
  `attempt_ceiling`, `result_never_contains` and the deny-bypass invariant go INCONCLUSIVE with
  `conversation_scope_unknown` instead of concluding from a partial view.
- **The deny-bypass invariant stays recording-level.** A scoped case for it is INCONCLUSIVE
  (`conversation_scope_invariant`): one conversation's slice cannot demonstrate a boundary-wide invariant.
- **A scoped existential claim whose tool only appears under another conversation does not FAIL.** Client-declared
  attribution is a claim, not an authenticated fact, so the recording cannot prove the call was *not* this
  conversation's; the outcome is the existing `no_calls` INCONCLUSIVE.
- **Recording integrity, structure and completeness are unaffected** by a scope: those gates run first and stay fatal.
