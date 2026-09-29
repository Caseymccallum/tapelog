# Tapelog Plugin API (WASM)

Plugins extend tapelog without forking it: extra policy judgment and
org-specific redaction, compiled to WebAssembly and run in a sandbox.

**Safety model:** plugins are *pure computation over JSON*. The runtime
grants **no host I/O** — a plugin cannot read files, open sockets, or
phone home. And the two things plugins can influence are constrained
host-side:

- **`verdict_hook` may only TIGHTEN decisions** (allow→confirm/deny,
  confirm→deny). A buggy or malicious plugin *cannot* weaken enforcement
  — the lattice check lives in the Go host, not in the guest.
- **`redact_hook` only ever sees already-redacted text** — it can add
  redactions but can never un-redact what it never received.

A plugin that crashes or returns garbage **fails closed** (the decision
tightens to `deny`).

## Loading

```bash
tapelog record --plugin ./myplugin.wasm --plugin ./orgguard.wasm -- policy…
tapelog mux    --plugin ./myguard.wasm --config mux.yaml …
```

Plugins run in the order given; each `verdict_hook` receives the previous
plugin's output. Runtime: [wazero](https://github.com/tetratelabs/wazero)
(pure Go — keeps tapelog a single static binary).

## Guest ABI

The module MUST be a **WASI reactor** — build Go plugins with
`GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared` (the host calls
`_initialize`, then keeps the module alive). Command-style modules with
`_start` are also accepted.

Required exports:

| Export | Signature | Meaning |
|---|---|---|
| `alloc` | `(size: i32) -> ptr: i32` | Allocate `size` bytes; return pointer |
| `free` | `(ptr: i32, size: i32)` | Release an input buffer (may be a no-op) |
| `verdict_hook` | `(in_ptr: i32, in_len: i32) -> out_ptr: i32` | Required |
| `redact_hook` | `(in_ptr: i32, in_len: i32) -> out_ptr: i32` | Optional |

- Input: UTF-8 JSON at `[in_ptr, in_ptr+in_len)` (host-allocated via `alloc`).
- Output: `out_ptr` points to a **NUL-terminated** UTF-8 JSON string that
  MUST stay valid until the next hook call (reuse a static buffer).
- Outputs are capped at 1 MiB.

## Hook contracts

### `verdict_hook`

Input:

```json
{"tool": "exec_shell", "args": {"cmd": "ls"},
 "verdict": "allow", "rule_id": "allow-reads", "reason": "read-only tools are safe"}
```

Output (partial objects fine — omitted fields keep their values):

```json
{"verdict": "confirm", "reason": "myplugin: exec needs eyes on it"}
```

Verdicts only strengthen: `allow < confirm < deny`. Returning a weaker
verdict is ignored (and logged). Returning `{}` means "unchanged".

### `redact_hook`

Input `{"text": "key ACME-abc123"}` → output `{"text": "key [REDACTED]"}`.
Applied to every string after tapelog's built-in redaction.

## Reference plugin

[`examples/plugins/argguard/`](../examples/plugins/argguard/) — a complete
guest in ~100 lines of Go demonstrating both hooks (deny on a marker in
args, tighten exec calls to confirm, mask org-specific tokens). Build with
`build.ps1`; the compiled `argguard.wasm` is committed for tests.

```powershell
cd examples/plugins/argguard
./build.ps1
tapelog record --plugin ./argguard.wasm --log s.jsonl -- <server>
```

## Writing a plugin

The ABI is language-neutral; any language that targets WASI can implement
it. Sketches:

**Go** — see the reference plugin: `//go:wasmexport alloc` etc.,
`unsafe.Slice` to read input, `json.Marshal` + NUL-terminated kept buffer
for output. Remember `-buildmode=c-shared`.

**Rust** — `#[no_mangle] pub extern "C" fn alloc(size: i32) -> i32` over a
`Vec<u8>` you forget (or a static buffer); return `CString::into_raw`
for output; build with `--target wasm32-wasip1` (reactor crate-type
`cdylib`).

Requirements for all plugins: deterministic (same input → same output —
the session log's stability depends on it), and fast (hooks run inline
on the mediation path; budget microseconds, not milliseconds).

## Versioning

The ABI is v1 and additive: new optional exports may appear; existing
signatures will not change. If a breaking change is ever needed, plugins
will be loaded through a versioned adapter.
