# Derived evidence — not the original recording

The two `.jsonl` files in this directory are **derived** from the M14.1 live gateway
recording. They are faithful to the session, but they are not the original bytes.

## What changed

| Field | Original | Here |
|---|---|---|
| `session/start.policy_id` | the recorder's absolute policy path on the experiment machine | `policy.yaml` |
| everything else | — | byte-identical, field for field |

Because a payload changed, **both files were re-chained**. A recording whose payload
changed and whose hashes did not would be exactly the kind of file Tapelog refuses to
serve.

## Heads, so you can tell them apart

| Artefact | Events | Chain head |
|---|---|---|
| original, open (as verified during M14.1) | 16 | `7eda1b89ff2418c8ba411232ee1af2ddcf918ffbc9d9fe3ee1ea2f8ecf30122b` |
| original, closed | 17 | `da4d0c04b8058d9122278bce07c8a97ecb2b5b0a37c6709f4a08723146e39e2a` |
| **derived**, open (`session-gateway-open.jsonl`) | 16 | `4a87cd5491c7894f37e83938a4bcf666617710ad4727c3867326ccd5141361ea` |
| **derived**, closed (`session-gateway-closed.jsonl`) | 17 | `ef6befe478f8660c5fa5b112f1f3c16832d6069e0a3bd11c0453aea7e8ef8a0e` |

`DERIVED.json` carries the same figures, and `TestGatewayEvidenceIsVerifiableAndHonest`
asserts that the committed files match them and that the derived head differs from the
original's. If someone re-chains these files again, that test fails.

The immutable originals stay outside the public tree, with the rest of the M14.1
evidence.

## What is still true of the derived pair

- The open file is a **byte prefix** of the closed one; the only extra event is
  `session/end`, and it chains onto the open head.
- Five mediated calls, four `allow` decisions and one `no-delete` deny, four recorded
  results (one error), two client-declared conversations, zero unattributed calls.
- Structure and completeness behave exactly as in the original: open → `INCOMPLETE —
  no_end`, closed → `VALID`.
