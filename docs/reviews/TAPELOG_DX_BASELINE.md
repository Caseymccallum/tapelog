# Tapelog DX baseline — recorded before implementation

Audit date: 2026-11-07. Canonical repository: `C:\Users\Casey\Web Apps\agent-boundary`; origin `Caseymccallum/tapelog`.
Local and live remote main: `f79d48a9c31fe1f50d7ccbf90dcd637f66b69caf`.
Release tag v0.4.0 resolves to `e65e28a6b53b536ff6fd30652d58fc552d29bfe1`.
Original checkout clean on main; dedicated branch `feat/developer-experience-evolution` created. No other branches/worktrees changed.
Go 1.27.0 windows/amd64. Baseline tests started; results pending.

## Findings from source and documentation

Read README, QUICKSTART, CLIENT-SETUP, ROADMAP, ARCHITECTURE, THREAT_MODEL, POLICY, TESTING and launch kit; examined CLI, doctor, TUI loader/story/view, dashboard and compatibility suite.

| Persona | Attempt / observed obstacle | Evidence limit |
|---|---|---|
| New to MCP | Quickstart assumes an installed harness; no-agent demo requires source, Go and a script; five-minute schedule is an aspiration | No fresh-machine or novice timing yet |
| Roo / Cline | Hand-edited nested JSON, Windows 8.3 workaround and early approval/security choices | No live client session tested |
| Claude Code / Desktop | Hand-edited JSON, relative logs, auto-confirm in starter examples | No live client session tested |
| Multiple-server developer | mux and compatibility lab exist; onboarding obscured by full command tour | Existing hermetic tests available |
| Security-conscious developer | Good threat model, verify-first replay and fail-closed boundary; default-allow vs all-activity recording needs clearer framing | Security regression suite pending |

`doctor` checks policy/plugins/platform/log-directory but cannot preflight a specified executable or port. Its log-directory inference treats an extensionless filename as a directory. It recommends auto-confirm in its no-terminal message. `inspect` silently skips invalid JSON lines; summary counts decisions as allowed/denied (not unique successful calls), omits failed results and does not explain zero calls. Dashboard uses textContent, CSP, Host pinning and CSRF controls; leave handlers unchanged. Session start does not identify upstream servers, so an honest overview cannot invent server attribution.

## Selected scope

DX1: noninteractive guided `setup` for an existing named stdio server in strict JSON. Default dry-run describes changes without printing credentials/argv; explicit output creates a new candidate file only, preserving other servers/settings and env. No apply, policy edits or client launches. Add executable and port preflight to doctor without starting servers.

DX2: inspection summary with scope, failed-result and discovery counts, missing-end explanation, integrity explicitly not checked here; reject malformed JSON rather than silently discard evidence. Add a summary-only flag using the same renderer. Prominent dashboard scope text, no security/API changes.

DX3: concise first-session guide and isolated local demo using existing fixture server, without auto-confirm or overwriting existing demo logs. Explain safe manual activation/undo and client/platform limits.

DX4: CLI acceptance tests plus full Go build/vet/tests, existing compatibility and security regressions, actual Windows paths-with-spaces workflow. Record unavailable native macOS/Linux and live-client gates. No five-minute claim.

Deferred: interactive config manager/apply/restore, desktop UI, transport/policy/replay changes and universal launcher workaround. None is necessary for this bounded milestone.
