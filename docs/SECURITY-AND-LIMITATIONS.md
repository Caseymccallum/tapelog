# Security model and limitations

This page says what Tapelog protects, what it merely observes, and where it can mislead you if you
read it too generously. The threat table itself lives in [THREAT_MODEL.md](THREAT_MODEL.md) and the
verdict semantics in [M9-EVIDENCE-SEMANTICS.md](M9-EVIDENCE-SEMANTICS.md) — this page does not
restate them.

## Mediation versus observation

**Authoritative, inside the boundary.** For traffic that crosses Tapelog, a `deny` verdict is decided
before any side effect: the call is not forwarded, and the JSON-RPC error names the rule, the reason
and the verdict. That rejection is recorded, and `inspect` reconstructs the causal story from it.

**Passive, outside the boundary.** Everything else is unobserved. An agent's built-in file tools, its
own network access, actions taken by the MCP server after it receives an allowed call, and anything
another process does — none of it is visible, policed, or recorded. A recording with no denials is
not evidence that nothing was denied elsewhere; it is evidence about one connection.

This distinction is the one most likely to be overstated. Tapelog cannot enforce policy on activity
outside the boundary it actually controls.

## What a verified chain does and does not prove

`tapelog verify` answers one question: were these bytes altered after they were written? It detects
modification, deletion and reordering of individual events, including edits with recomputed hashes.

It does **not** prove authorship, completeness, or that the recorded calls are all the calls that
happened. A zero-byte file, a startless file and a two-identity file all still report `chain intact`,
because that is a statement about bytes. Four properties are deliberately separate:

1. **Integrity** — the chain recomputes.
2. **Structure** — one session id, one leading `session/start`, at most one trailing `session/end`.
3. **Completeness** — the session's own end was observed.
4. **Eligibility** — whether *this particular claim* may be decided from this evidence.

`verify` answers (1) by default and prints (2) and (3) beside it; `--require-structure` makes them
fatal; `inspect --check` exits 3/4/5 for integrity/structure/incomplete; `replay` gates on them;
`lab` applies all four to every claim.

## What recordings contain

Recorded tool arguments and results are stored **as recorded**. If an MCP server returns a credential
or personal data, that value is in the log.

- Redaction is applied to secret-shaped arguments and results before hashing, and redacted content is
  marked rather than silently dropped. Redaction is key- and pattern-based: it cannot know your
  domain's sensitive fields.
- **Result bodies are redacted by key, not just by pattern.** A JSON document returned as text — the
  usual shape of an MCP result — is parsed and masked with the same sensitive-key rules as arguments,
  including nested objects, arrays, JSON escaped inside JSON, and a document carried as a JSON string.
  A credential pattern elsewhere in the document does not stop the key pass. This is bounded and
  defined: documents up to 256 KiB are parsed, JSON nested more than 24 string layers deep is not
  descended into, and anything that is not one well-formed JSON document (prose, malformed JSON, a
  document with trailing text) is left to the pattern pass only. Two consequences of those bounds,
  stated because they surprise people: escaping makes each added string layer roughly double the
  encoded size, so the 256 KiB limit usually binds well before 24 layers; and *structural* nesting
  inside one document is bounded only by the JSON decoder, after which the whole value falls back to
  the pattern pass. Past any bound the value is still recorded, still pattern-redacted, and still
  readable.
- **What is still not protected.** A secret with no recognisable shape in free text under an innocuous
  key is stored as recorded: `{"note": "the phrase is …"}` inside prose, a bare value with no
  credential pattern, or a key name that suggests nothing. Tapelog does not claim to find arbitrary
  secrets in prose.
- **If the output cannot be redacted, do not record it.** `tapelog record --omit-result-text` keeps the
  shape of every result and replaces everything it does not recognise as structure with
  `[REDACTED] (result text omitted at record time)` — before hashing, so the bytes never reach the disk
  and replay reproduces exactly what was kept. The rule is fail-closed: only a fixed list of structural
  keys (`type`, `uri`, `name`, `role`, `code`, `id`, `isError`, …) and a small list of measurement keys
  (`tokens`, `size`, `bytes`, `count`, `duration`, …) survive; every other string, and every number
  under any other key, is blanked. Content whose whole point is the payload — prose, oversized output,
  an unknown schema — therefore has nowhere to hide. Result-based regression claims then report
  `redacted_content` instead of a verdict.
- Recordings are local files. New per-start recordings are created `0600` in a `0700` directory on
  Unix-like systems; Windows access follows the directory ACL, so choose a private directory.
- Large payloads can be offloaded to a content-addressed blob store next to the log
  (`--blob-threshold`); the blobs are as sensitive as the log.
- The workbench renders recorded content. Anything that reaches a terminal or a page is passed through
  the escaping path first, and the workbench binds to loopback with a strict CSP and an optional
  bearer token.

Treat a recordings directory as you would treat shell history plus tool output.

**For a first external evaluation, use fictional data.** Recording real confidential traffic is
defensible only when one of these is true: the tool returns JSON and you accept the bounds above, or
you run with `--omit-result-text` so no payload is stored at all. Prose results and unrecognised key
names are still recorded as received — that is the remaining gap, and it is why this document says
"treat every recording as a secret" rather than "redaction removes secrets".

## Attribution is a claim, not an identity

`conversation_id`, `tool_call_id` and `subagent_id` are **client-declared** values copied from request
`_meta`. They group activity. They are not authenticated, they are not a session boundary, they never
create or close a session, and they are validated as bounded opaque tokens (a bad value is dropped,
never truncated). A scoped regression case narrows the evidence, it does not establish who was talking.
See [CONVERSATION-ATTRIBUTION.md](CONVERSATION-ATTRIBUTION.md).

## Replay

- Replay refuses evidence its default policy rejects: a broken chain, a file that is not one complete
  valid session, or request ids shared by more than one outstanding call.
- Each refusal names its escape hatch (`--allow-unverified`, `--allow-invalid-structure`,
  `--allow-ambiguous-ids`), and every override now prints a warning saying what was overridden.
- Replay serves the **modern** protocol era regardless of the era the recording was made in, and
  synthesises the handshake itself; tape content is served verbatim. That is a deliberate choice for a
  hermetic double, not a reproduction of the original negotiation.
- A denied call has no recorded result, so repeating it in replay is a **miss** (`-32011`), not a
  synthesised denial.

## Resource limits

| Surface | Limit | Behaviour past it |
|---|---|---|
| `tapelog lab` | 10,000 events | INCONCLUSIVE `damaged_recording` |
| workbench (`web`) | 50,000 events, 16 MiB per file | file reported as `limited`; use the CLI |
| `behaviour` analysis | 120 files, 48 MiB total, 16 MiB / 50,000 events per file | caps reported in the coverage object |
| library listing | 200 newest files from 10,000 directory entries | `capped` flag in the response |

## Supply chain

At this commit `govulncheck ./...` reports **no vulnerabilities**, with the Go toolchain pinned at
1.27.3 and `golang.org/x/*` updated accordingly. The release pipeline is configured to build static
binaries (`CGO_ENABLED=0`), emit one SBOM per archive, and sign checksums with keyless cosign via the
workflow's OIDC identity — see [SECURITY.md](../SECURITY.md).

**For the 0.5.0-beta.1 binaries, the supply chain is checksums and nothing more.** The release workflow
is configured to sign `checksums.txt` with keyless cosign and to emit an SBOM per archive; neither was
produced or verified for this beta, and no tag exists. Verify downloads against the published
`checksums.txt`, and treat the signature claims in the workflow as future work, not present fact.

## Reporting

Security reports: see [SECURITY.md](../SECURITY.md). Please do not open a public issue for a
vulnerability.

## Known limitations, stated plainly

1. No macOS build has been functionally tested.
2. The Regression Lab refuses recordings over 10,000 events; verify, inspect, replay and browse do not.
3. A `confirm` policy rule with no terminal attached fails closed and looks like a hang.
4. The product name collides with an unrelated MCP project in the same ecosystem.
5. Built-in client tools are invisible to every surface; a "clean" session may still have done
   anything the client does natively.
