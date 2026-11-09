# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

- Use **GitHub Security Advisories → "Report a vulnerability"** on this repository. It is the only
  private channel wired for this beta; there is no security mailbox to fall back on, and a public
  issue is not a private report.

Include: description, reproduction steps, impact, and any suggested fix. We acknowledge within **72 hours** and aim to ship fixes within **14 days** for high-severity issues. Credit is given unless you prefer anonymity.

## Supported versions

| Version | Supported |
|---|---|
| `main` (pre-1.0) | ✅ best effort |
| tagged releases | ✅ latest tag only |

## Security posture (what we commit to)


## Verifying releases

Release artifacts are **keyless-signed with cosign** (GitHub OIDC) and ship
per-archive **SBOMs** (syft/SPDX). Every release includes `checksums.txt`,
`checksums.txt.sig`, and `checksums.txt.pem`.

```bash
# 1. Verify the checksum file's signature (keyless):
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp "https://github.com/Caseymccallum/tapelog/.github/workflows/release.yml@.*" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# 2. Verify the artifact you downloaded:
sha256sum -c checksums.txt --ignore-missing

# 3. Inspect the SBOM for your platform's archive:
syft convert tapelog_X.Y.Z_linux_amd64.tar.gz.spdx.json -o table
```

The signing identity is the release workflow itself — a signature is only
as trustworthy as the repository's CI. Report suspected supply-chain
issues via the private channel above.

**Prereleases can differ, and the archive says which you have.** A build made by
the release workflow is signed and carries an SBOM whatever its version label. A
`-beta`/`-rc` archive published by hand from a local build carries
`checksums.txt` and the `MANIFEST.txt` that names its commit, but **no
signature and no SBOM**; its `MANIFEST.txt` states `signing: NOT PERFORMED`.
Check the archive before trusting the stronger claim, and treat an unverified
prerelease as untrusted code like any other unsigned download.

## Verifying session evidence

Session logs are hash-chained, but a chain alone does not prove *who*
attested it or *when* (see `spec/FAQ.md`). `tapelog checkpoint` closes
that gap: signed checkpoints (SSH key or cosign) with a **transparency-
log witness** (Rekor) make "this trajectory existed in this exact form"
verifiable. Checkpoint verification is fully offline: entry binding,
signed entry timestamp, RFC 6962 inclusion proof, and signed tree head
against the pinned log key.

```bash
tapelog checkpoint session.jsonl --signer ssh --key ~/.ssh/id_ed25519
tapelog verify session.jsonl --checkpoint session.jsonl.checkpoint.json
```

A checkpoint created with `--witness none` verifies its signature but
`verify` warns loudly: signed but NOT witnessed — the "when" is unproven.
See `docs/CHECKPOINTS.md`.

Per docs/THREAT_MODEL.md, tapelog is a security-adjacent tool. We therefore hold ourselves to:

- **Signed releases** (Sigstore/cosign) + published checksums — **automated**:
  every `v*` tag runs the `release` workflow (goreleaser + keyless cosign
  over `checksums.txt`; verify with the cosign invocation above)
- **SBOM** per archive (syft/SPDX) + **GitHub build provenance attestation**
  — **automated** in the same workflow (`workflow_dispatch` backfills
  older tags)
- **No telemetry, ever.** Nothing is collected or reported, and Tapelog makes no network call on its own: it writes local files and talks to the MCP servers you configure. Two commands can reach the network, and only when you run them — `tapelog checkpoint` submits the checkpoint's chain head to a public transparency log (Rekor is the default; `--witness none` keeps it local), and `tapelog export otel <session.jsonl> --endpoint <url>` sends OTLP traces to the endpoint you name.
- **Dependency hygiene:** automated updates, `govulncheck` in CI
- **Prompt-response:** security fixes are released before public disclosure timelines typical for feature work

## Scope of claims

A reminder: tapelog constrains and records agent *tool calls* mediated through its proxy. It is not a sandbox and not a prompt-injection filter. See [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the exact, honest boundaries of the security claims.
