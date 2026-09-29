# Naming research & availability audit

**Date:** 2026-09-29 · Method: live registry API queries (npm, PyPI, crates.io), GitHub org pages, RDAP domain lookups (`rdap.org` bootstrap → registry RDAP), web search for brand conflicts. All results verified live this date.

## Reality check

Every short dictionary word in our semantic space (record / replay / tape / trace)
is registered on every major registry — many by **name-squatting stubs and
adjacent products**. Notable examples: `kassette` on npm is an explicit
"name reservation" by a product called Kasset; `cassette.dev` was registered
**two days before this audit** (2026-09-27); the `cassette-dev` GitHub org
runs a product that "records API behavior" (adjacent concept).

## Decision matrix

| Candidate | npm | PyPI | crates.io | GitHub org | .dev domain | Concept conflicts |
|---|---|---|---|---|---|---|
| `cassette` | ❌ dormant playlist lib (2018) | ❌ Uber "Cassette" HTTP record-replay (2014) | ❌ async executor (semi-active) | ❌ `cassette-dev` exists | ❌ registered 2026-09-27 | **High** — "record API behavior" product |
| `spool` | ❌ dormant | ❌ mailer (2024) | ❌ **active** dev tool (Sep 2026) | ❌ | — | Medium |
| `capstan` | ❌ **active** VPS lib | ❌ **active** webhook TUI | ❌ CAD lib | ❌ | — | Low |
| `cartridge` | ❌ dormant gulp scaffold | ❌ Django e-commerce | ❌ emulator | — | ❌ | Low |
| `kassette` | ❌ squatted ("name reservation") | — | — | — | ❌ | **High** (Kasset product) |
| **`tapelog`** | ✅ **free** | ✅ **free** | ✅ **free** | ⚠️ held by tapelog.ai (fitness wearable + typing app — unrelated consumer products) | ✅ **free** | **Low** |
| `unspool` | ❌ squat stub (0.1.0) | ✅ free | ✅ free | ⚠️ empty org ("test-repo") | ❌ | Low |
| `kapstan` | ✅ free | — | ✅ free | — | ❌ (2024) | Low |

## Recommendation: **tapelog**

The only name that is **free on all three package registries** and has a
**free `.dev` domain** (`tapelog.dev` — register immediately). Brand
conflicts are consumer products in unrelated spaces (fitness wearable,
typing tutor), not developer tooling.

### Claim plan for `tapelog`
| Asset | Action |
|---|---|
| `tapelog.dev` | Register **today** (cheap, instant) |
| GitHub org | `tapelog-oss` or `tapelog-dev` (bare `tapelog` held by unrelated product; GitHub's rename/drop policy can reclaim squat later) |
| Go module | `github.com/<org>/tapelog` |
| Binary | `tapelog` |
| npm adapter | `tapelog-mcp` (free — verify at publish) |
| PyPI adapter | `tapelog-mcp` (free — verify at publish) |
| Rust plugin SDK (future) | `tapelog-plugin` (free — verify at publish) |

### Fit with the product

"tapelog" is descriptive and self-explanatory: the tool creates **session
logs** (our spec is literally the *Agent Session Log Format*) and tapes
record/replay heritage. Reads instantly as what it is — the "ripgrep/
lazygit" naming school, which suits a sharp CLI dev tool.

### Rename scope (if adopted)

Mechanical ~1h pass: Go module path + binary name + docs + the
`replay.Cassette` type (→ `replay.Log` / `Tape`) + examples + fixtures.
Conformance vectors regenerate unchanged (format name is neutral — the
spec is the "Agent Session Log Format", not cassette-specific).

## Runner-ups if `tapelog` is rejected

1. **Keep `cassette`** with org `cassette-replay`/`cassette-oss`, accept
   dormant-registry collisions + the adjacent `cassette-dev` product, skip
   `.dev` (use `cassette.sh`/`cassette.tools` — unverified). Strongest
   metaphor, messiest legal/discovery picture.
2. **Coined name round** — generate distinctive coinages (unclaimed, ownable
   trademarks) and audit them the same way.
3. `unspool` — good verb heritage; clean PyPI/crates; npm has a squat stub
   we could request from npm support.
