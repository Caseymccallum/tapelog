# The Regression Lab

A regression case asserts something about recorded evidence and gets one of three answers:

| Verdict | Meaning | Exit |
|---|---|---|
| **PASS** | eligible evidence satisfies the assertion | 0 |
| **FAIL** | eligible evidence **contradicts** it | 1 |
| **INCONCLUSIVE** | the evidence cannot decide it — not a failure, not a pass | 2 |

The distinction is the product. A tool that collapsed FAIL and INCONCLUSIVE would be unusable for
release gating; a tool that upgraded INCONCLUSIVE to PASS would be worse. Every inconclusive result
carries a **code** naming why.

## Claims you can make

| Claim kind | Assertion | Incomplete/ambiguous evidence |
|---|---|---|
| `attempted` | a call to this tool crossed the boundary | INCONCLUSIVE |
| `no_attempts` | no call to this tool crossed the boundary | INCONCLUSIVE (see eligibility modes) |
| `attempt_ceiling` | at most N calls to this tool | INCONCLUSIVE |
| `denied` | a call to this tool has a deny decision | INCONCLUSIVE |
| `answered` | a call to this tool has a recorded result | INCONCLUSIVE |
| `result_contains` | a recorded result contains this text | INCONCLUSIVE |
| `result_never_contains` | no recorded result contains this text | INCONCLUSIVE |
| `sequence` | these tools were called in this order | INCONCLUSIVE |
| `no_deny_bypassed` | a denied call never went on to execute | INCONCLUSIVE |

Measured behaviour on real recordings (23 cases, `docs/releases/M15_REPLAY_REGRESSION_ACCEPTANCE.md`):

- `attempted` on a tool that **never appears** → INCONCLUSIVE `no_calls`, not FAIL.
- `denied` on a call that was **allowed** → FAIL. The evidence proves the assertion false.
- `answered` on a **denied** call (no result by design) → INCONCLUSIVE `no_results`.
- `no_deny_bypassed` on a recording with **no denials at all** → INCONCLUSIVE `no_denials`: silence
  cannot demonstrate an invariant.

## The two eligibility modes

By default, an *absence* claim is counted, not concluded from silence: if the tool never appears,
there are zero calls to count and the answer is INCONCLUSIVE. If you have one complete, unambiguous
session and you want to treat it as a closed world, opt in per case:

```json
{ "version": 1, "title": "delete_file never crossed this boundary",
  "evidence": "session.jsonl", "eligibility": "complete-boundary",
  "expectations": [ { "kind": "no_attempts", "tool": "delete_file" } ] }
```

Under `complete-boundary` the same claim **PASSes** on a recording with no such call, and an
`attempted` claim on an absent tool **FAILs**. That is the documented difference — not a bug, and not
a licence to upgrade result-dependent claims: a claim that needs a result still goes INCONCLUSIVE
when results are ambiguous, redacted or missing.

## Codes you will see

| Code | Meaning |
|---|---|
| `no_calls` | the tool never appears in this evidence |
| `no_results` | calls exist but none has a recorded result |
| `redacted_content` | a result was redacted, so text claims cannot be decided |
| `ambiguous_correlation` | a result cannot be tied to the call that asked for it |
| `missing_decision` | a call has no recorded policy decision |
| `no_denials` | the invariant needs a deny, and none was recorded |
| `short_sequence` | fewer recorded results than the sequence requires |
| `incomplete_recording` | no complete `session/start` … `session/end` |
| `structure_invalid` | not one valid session log |
| `damaged_recording` | chain invalid, empty, or above the 10,000-event limit |
| `revision_changed` | the evidence differs from the revision saved with the case |
| `conversation_absent` | the scoped conversation does not appear |
| `conversation_scope_unknown` | unplaceable activity could belong to the scope |
| `conversation_scope_invariant` | a recording-level claim cannot be judged from one conversation |

## Conversation scope

A case may name a client-declared conversation id (`"conversation": "demo-beta"`). The scope narrows
the evidence **before** any claim is decided, and it can only remove evidence:

- the claim is judged on that conversation's calls and nothing else;
- an id that never appears is `conversation_absent`, never an empty-but-observed conversation;
- unplaceable calls (no declared id, a rejected id, an ambiguous pairing) make absence claims
  `conversation_scope_unknown`;
- recording-level invariants (`no_deny_bypassed`) are refused under any scope.

Conversation ids are **client-declared claims**, not authenticated identities.

## Evidence gates

Before any claim is evaluated, the evidence must be one complete valid session with an intact chain,
under **10,000 events** (`damaged_recording` above that). Larger recordings can still be verified,
inspected, replayed and browsed — only the lab refuses them.

## Writing and running cases

The browser is the authoring path: open a recording, select a call, use **Create case**, pick a rule
in plain language, add a conversation scope if you want one, preview the outcome, then save. Cases are
plain JSON beside your recordings.

A case is a small file you can also write by hand:

```json
{
  "version": 1,
  "title": "A permitted read reached the boundary",
  "evidence": "session-two-conversations.jsonl",
  "expectations": [ { "kind": "attempted", "tool": "read_file" } ]
}
```

Run one case, or a suite of them:

```bash
tapelog lab run --dir examples/regression-demo --case 03-interrupted-inconclusive.json
tapelog lab run --dir examples/regression-demo --case 03-interrupted-inconclusive.json --json
tapelog lab suite --dir examples/regression-demo --suite demo-suite.json
```

`--json` prints the full result including each check's code and detail sentence. A working five-case
example that produces PASS, FAIL and INCONCLUSIVE deliberately ships in
[`examples/regression-demo/`](../examples/regression-demo/README.md), together with a generator that
rebuilds its recordings through the real boundary.

## What a case cannot prove

- Only MCP-mediated traffic is recorded; built-in agent tools never appear.
- A case decides recorded facts. It does not re-execute tools, and it cannot show that a passing
  recording reflected correct behaviour — only that the recorded behaviour matched the assertion.
- A denied call is never replayed and has no result; claims that need a result will say so.
