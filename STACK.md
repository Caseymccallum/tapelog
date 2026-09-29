# Stack Decision: Language & Bill of Materials
**Decided:** 2026-09-29 · Companion to `RESEARCH.md`

## 1. Decision: **Go** (Rust = credible runner-up, kept as plugin language later)

### Evidence (community preference)
| Signal | Finding |
|---|---|
| Official MCP SDKs | **Go SDK is official + Google-collaborated** (`modelcontextprotocol/go-sdk`, 5.2k★, v1.7.x active). Rust SDK official too (`rmcp`, 3.9k★). Both viable; neither is excluded. |
| MCP infrastructure layer | Official MCP **registry service is Go** (7.3k★). Gateways/sidecars (Stacklok ToolHive), security runners (OPA, gitleaks, trivy, cosign) — **overwhelmingly Go**. |
| Agent harness split | TS = mass-market (Gemini CLI, Claude Code, opencode/Bun, Cline); **Rust = performance-first flags** (Codex CLI ~97% Rust, Goose); Go present (Charm Crush 28k★). |
| Contributor friction | Go's fast builds + gentle learning curve are the documented low-friction path; Rust compile times are a known contribution complaint (Prossimo/Rakic commentary). For a project optimizing for outside contributions, Go wins. |
| Distribution model | goreleaser single static binaries (macOS arm64/x64, Linux, Windows) = same model as `gh`, `lazygit`, `act`, `cosign`. Rust also fine here via cargo-dist. |
| Counter-case for Rust | Safety branding + stars; best if we later do in-path TLS interception or high-throughput replay (rustls/pingora). Codex/Goose prove Rust agent-infra can win. |

**Bottom line:** Rust gives stars and safety branding; **Go gives the community that actually maintains MCP registries, gateways, and policy engines — and the lowest-friction path to contributions.**

### Community-recruitment strategy (language ≠ audience)
The server-author community is ~TS/Python. Mitigate Go's distance from them:
1. Ship **TypeScript + Python adapter packages** (config + integration only) so ecosystem devs plug in without touching Go.
2. Later: **WASM plugin API** — recruits Rust/TS contributors to the periphery without touching core.
3. Publish the **session-log schema as a language-neutral spec** — the real interop surface.

### Policy engine note
Keep **Cedar** as primary (explainable deny is our differentiator) — now strong in Go: `cedar-policy/cedar-go` is official-org (v1.8.x active) and **Cedar joined CNCF as a sandbox project**. Put policy behind a small `Evaluator` interface so a Rego escape hatch can be added later if the security crowd demands OPA.

---

## 2. Bill of Materials — reuse aggressively

### Core (reuse as-is)
| Need | Component | License | Maturity | Notes |
|---|---|---|---|---|
| MCP client + server roles (proxy = both) | `modelcontextprotocol/go-sdk` | MIT | official, v1.7.x active | Low-level JSON-RPC layer also usable; verify 2026-07-28 spec support at integration. Fallback: `mark3labs/mcp-go` (largest 3rd-party ecosystem). |
| Policy decisions | `cedar-policy/cedar-go` | Apache-2.0 | official Cedar org, v1.8.x; Cedar = **CNCF sandbox** | `is_authorized` + diagnostics → explainable deny for free. `cedar-policy/cedar-wasm` as future plugin path. |
| Traces / OTel export | `opentelemetry/opentelemetry-go` + `otel-go-contrib` | Apache-2.0 | mature (Go = OTel reference impl) | GenAI semconv attributes mapped on our spans. |
| CLI | `spf13/cobra` | Apache-2.0 | industry standard | `gh`, `trivy`, `cosign` all built on it. |
| TUI (viewer/scrubber) | `charmbracelet/bubbletea` + `lipgloss` + `bubbles` | MIT | 30k★+ ecosystem | Also `charmbracelet/huh` for confirm prompts (require-confirm flow). |
| Config (policy file) | `gopkg.in/yaml.v3` + `spf13/viper` (or koanf) | MIT | mature | Human-friendly YAML front-end compiling to Cedar. |
| Embedded index for cassettes | `modernc.org/sqlite` (pure-Go, no cgo) | BSD-3 | active | JSONL remains source of truth; sqlite = queryable index only. Alternative: `etcd-io/bbolt`. |
| JSON Schema validation (session schema) | `santhosh-tekuri/jsonschema` | Apache-2.0 | mature | Validate every log entry at write + replay time. |
| Secret scanning in logs | `gitleaks/gitleaks` `detect` package (as library) | MIT | 3.8k★, battle-tested | Regex/pattern redaction engine reuse; plus our own MCP-context patterns. |

### Build small (justified)
| Need | Why build | Size |
|---|---|---|
| Session event schema + hash chain | **This is the product's core IP** — the standard others adopt (AOS v0.1 is nearest prior; not replay-grade) | ~schema + 50 LOC chaining |
| Canonical arg hashing for replay matching | Need JCS-style canonical JSON: `ucarion/jcs` or `deszhou/jcs` to build on; matching-rule UX is ours | small |
| Replay engine (cassettes, fail-loud, policy what-if) | No Go/Rust equivalent of vcrpy exists — the whitespace itself (VCR semantics port) | the main work |
| MCP transparent interception plumbing | go-sdk handles transport; the in-line verdict hook + descriptor pinning is ours | medium |

### Supply chain & quality (adopt from day one)
| Need | Component |
|---|---|
| Release automation | `goreleaser/goreleaser` (macOS arm64/x64, Linux, Windows static binaries) |
| Signed releases | `sigstore/cosign` + checksums |
| SBOM | `anchore/syft` |
| Security scorecard | OpenSSF Scorecard (weekly) |
| CI hardening | golangci-lint, `govulncheck`, OSS-Fuzz/ClusterFuzzLite (Go supported) |
| Dep updates | Dependabot / renovate |

### Explicitly rejected / deferred
- **OPA/Rego as user-facing policy language** — poor DX for this audience; keep as later escape hatch behind `Evaluator` interface.
- **AGPL** — Daytona dead-end lesson; **ELv2** — Phoenix's non-OSI trap. Use **Apache-2.0**.
- Building an eBPF/kernel layer (Meta mcpguard-dynamic territory) — v3+; v1 is userland proxy, defense-in-depth later via Landlock/seccomp hooks.
- Any hosted/telemetry-phone-home component — local-first is the brand.

## 3. What this means for week 1
1. Scaffold Go module (`cobra` CLI skeleton: `record`, `replay`, `verify`, `policy test`), goreleaser config.
2. Implement session-event schema v0 + JSONL hash-chained writer (validated by jsonschema lib).
3. Wire `modelcontextprotocol/go-sdk` proxy pass-through with recording hooks; redact via gitleaks detect.
4. Governance docs + threat model (from RESEARCH.md §4) land in-repo before first commit of real code.

