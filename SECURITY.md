# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

- Use **GitHub Security Advisories → "Report a vulnerability"** on this repository (preferred), or
- Email: `security@tapelog.dev` *(placeholder — wire before launch)*

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

Per docs/THREAT_MODEL.md, tapelog is a security-adjacent tool. We therefore hold ourselves to:

- **Signed releases** (Sigstore/cosign) + published checksums — **automated**:
  every `v*` tag runs the `release` workflow (goreleaser + keyless cosign
  over `checksums.txt`; verify with the cosign invocation above)
- **SBOM** per archive (syft/SPDX) + **GitHub build provenance attestation**
  — **automated** in the same workflow (`workflow_dispatch` backfills
  older tags)
- **No telemetry, ever.** The tool makes no network calls except to MCP servers you configure.
- **Dependency hygiene:** automated updates, `govulncheck` in CI
- **Prompt-response:** security fixes are released before public disclosure timelines typical for feature work

## Scope of claims

A reminder: tapelog constrains and records agent *tool calls* mediated through its proxy. It is not a sandbox and not a prompt-injection filter. See [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the exact, honest boundaries of the security claims.
