# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

- Use **GitHub Security Advisories → "Report a vulnerability"** on this repository (preferred), or
- Email: `security@cassette.dev` *(placeholder — wire before launch)*

Include: description, reproduction steps, impact, and any suggested fix. We acknowledge within **72 hours** and aim to ship fixes within **14 days** for high-severity issues. Credit is given unless you prefer anonymity.

## Supported versions

| Version | Supported |
|---|---|
| `main` (pre-1.0) | ✅ best effort |
| tagged releases | ✅ latest tag only |

## Security posture (what we commit to)

Per docs/THREAT_MODEL.md, cassette is a security-adjacent tool. We therefore hold ourselves to:

- **Signed releases** (Sigstore/cosign) + published checksums *(from first tagged release)*
- **SBOM** attached to releases
- **No telemetry, ever.** The tool makes no network calls except to MCP servers you configure.
- **Dependency hygiene:** automated updates, `govulncheck` in CI
- **Prompt-response:** security fixes are released before public disclosure timelines typical for feature work

## Scope of claims

A reminder: cassette constrains and records agent *tool calls* mediated through its proxy. It is not a sandbox and not a prompt-injection filter. See [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the exact, honest boundaries of the security claims.
