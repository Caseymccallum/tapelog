# Session Log Format — FAQ

## Adoption

**Q: Can I implement this without tapelog?**
Yes — that's the point. `session-log-v0.md` is normative and
self-contained; `test-vectors/` pins the exact bytes. Any language that
can produce canonical JSON and SHA-256 can be a conformant Writer.

**Q: What conformance class should I implement first?**
Writer (emit events), then Verifier (`verify` is ~100 lines and is where
tamper evidence becomes real). Reader/Replayer are optional.

**Q: Is the format stable?**
`v: 0` with additive-only evolution: unknown fields must be ignored
(§9). The canonical form is frozen by the conformance vectors; changing
it is a breaking `v` bump, not a quiet edit. Treat `v: 0` as "readers
must be lenient", not "everything changes".

**Q: JSONL — really? Why not protobuf/Avro?**
Evidence tooling values: diffable, grep-able, append-only, recoverable
after a crash mid-write, readable in an emergency without the schema
registry. Size is a real tradeoff (see FAQ: large payloads).

## Security

**Q: Is a hash chain enough for an audit?**
It detects tampering by anyone who doesn't recompute the whole chain
from the start. It does NOT detect truncation of the tail (keep the
final seq somewhere else), and it's not a signature — anyone can rewrite
everything and recompute. For non-repudiation, anchor `chain_head`
externally (append-only log, sigstore, notary) — the field exists for
that.

**Q: What about secrets in args/results?**
Writers SHOULD redact before recording (tapelog redacts + records the
fact). The format can't help if you write secrets into it — treat
session logs with the sensitivity of the sessions they record.

**Q: My payloads are huge.**
Store `args`/`result` out-of-band and record a digest reference — the
format defines a placeholder for exactly this (session-log-v0.md §6.2,
`{"$blob": {"sha256": …, "size": …}}`; tapelog: `record --blob-threshold
N`, content-addressed `<log>.blobs/` store). The hash chain binds the
digest, so swapped blobs are detectable; note replay then needs the
blob store (fail-loud on missing/mismatched blobs).

## Process

**Q: How do I contribute a conformance vector?**
Add a case + regenerate with `go run ./tools/gen-vectors`; a PR that
edits `vectors.json` by hand gets rejected. See ../CONTRIBUTING.md.

**Q: Who decides changes?**
Schema changes follow ../GOVERNANCE.md. The bar for `v: 0` changes:
additive only + vectors updated + two maintainers. The bar for `v: 1`:
a community process in a standalone repo (see ROADMAP).

**Q: Why not just OpenTelemetry?**
See [OTel-COMPARE.md](OTel-COMPARE.md) — they compose; the session log
is evidence + regression, OTel is observability.

**Q: Will you add <X>?**
Check docs/ROADMAP.md first (spans as first-class events, OTLP ingest
are listed there; blob references shipped — §6.2). File an issue with
the use case — formats grow from use cases, not feature matrices.
