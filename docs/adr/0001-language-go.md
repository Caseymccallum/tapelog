# ADR 0001: Core language is Go

**Status:** Accepted · 2026-09-29

## Context

Tapelog wants maximum adoption and outside contributions in the MCP/agent-tooling ecosystem. Candidates: Go and Rust. Full evidence in [STACK.md](../../STACK.md).

## Decision

Build the core (proxy, policy, session log, CLI, TUI) in **Go**.

## Rationale

- The MCP *infrastructure* layer is Go-flavored: official **Go SDK is Google-collaborated**; the official MCP registry service is Go; gateways/security runners (ToolHive, OPA, gitleaks, trivy, cosign) are Go.
- Contributor friction: fast builds, gentle learning curve — the documented low-friction path, critical for a project optimizing for outside contributions.
- Distribution: goreleaser static binaries, same model as `gh`/`lazygit`/`act`.
- Rust remains the **plugin/extension language** (WASM) and the fallback if we later need in-path TLS interception or high-throughput replay (rustls/pingora precedent).

## Consequences

- TS/Python ecosystem authors are one hop away → ship thin adapters + language-neutral schema spec.
- We accept less "safety branding" than a Rust project; compensated by defense-in-depth design and signed releases.
