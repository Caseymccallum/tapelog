// Package plugin implements cassette's WASM plugin API (docs/PLUGINS.md).
//
// Plugins are pure computation over JSON: no host I/O is granted, so a
// plugin cannot phone home, read files, or escape its sandbox. Two hooks
// are supported:
//
//   - verdict_hook: may TIGHTEN a decision (allow→confirm/deny,
//     confirm→deny). The tighten-only rule is enforced host-side — a
//     buggy or malicious plugin cannot weaken enforcement.
//   - redact_hook: contributes extra redaction on already-redacted text
//     (it never sees the original secrets).
//
// Guest ABI: the module MUST export alloc(i32)->i32 and free(i32,i32);
// hooks take (in_ptr, in_len) and return a pointer to a NUL-terminated
// UTF-8 JSON string that stays valid until the next hook call.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	fnAlloc   = "alloc"
	fnFree    = "free"
	fnVerdict = "verdict_hook"
	fnRedact  = "redact_hook"

	maxOutputBytes = 1 << 20 // 1 MiB cap on any hook output
)

// Request is the verdict_hook input (JSON).
type Request struct {
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Verdict string          `json:"verdict"`
	RuleID  string          `json:"rule_id"`
	Reason  string          `json:"reason"`
}

// Response is the verdict_hook output (JSON). Partial objects are fine:
// omitted fields keep their current values.
type Response struct {
	Verdict string `json:"verdict,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// rank orders verdicts by strength for the tighten-only rule.
func rank(verdict string) int {
	switch verdict {
	case "allow":
		return 0
	case "confirm":
		return 1
	case "deny":
		return 2
	}
	return 2 // unknown verdicts are treated as strongest (fail-closed)
}

// TightenAllowed reports whether a plugin may move from -> to.
// Plugins may only make decisions MORE conservative.
func TightenAllowed(from, to string) bool {
	return rank(to) > rank(from)
}

// Plugin is one loaded WASM module.
type Plugin struct {
	name      string
	runtime   wazero.Runtime
	mod       api.Module
	allocFn   api.Function
	freeFn    api.Function
	verdictFn api.Function
	redactFn  api.Function // nil when the plugin has no redact_hook
	mu        sync.Mutex
}

// Load instantiates a plugin from a .wasm file.
func Load(ctx context.Context, path string) (*Plugin, error) {
	wasm, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin %s: %w", path, err)
	}
	r := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, err
	}
	name := filepath.Base(path)
	// Plugins are WASI *reactor* modules (go build -buildmode=c-shared):
	// the Go runtime initializes in `_initialize` and never proc_exits.
	// Command-style modules with `_start` also work (skipped if absent).
	mod, err := r.InstantiateWithConfig(ctx, wasm, wazero.NewModuleConfig().
		WithName(name).
		WithStartFunctions("_initialize", "_start"))
	if err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("instantiate plugin %s: %w", name, err)
	}

	p := &Plugin{name: name, runtime: r, mod: mod}
	if p.allocFn = mod.ExportedFunction(fnAlloc); p.allocFn == nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: missing export %s", name, fnAlloc)
	}
	if p.freeFn = mod.ExportedFunction(fnFree); p.freeFn == nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: missing export %s", name, fnFree)
	}
	if p.verdictFn = mod.ExportedFunction(fnVerdict); p.verdictFn == nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: missing export %s", name, fnVerdict)
	}
	p.redactFn = mod.ExportedFunction(fnRedact) // optional
	return p, nil
}

// Name returns the plugin's file name.
func (p *Plugin) Name() string { return p.name }

// HasRedact reports whether the plugin implements redact_hook.
func (p *Plugin) HasRedact() bool { return p.redactFn != nil }

// Close tears down the module and its runtime.
func (p *Plugin) Close() error {
	return p.runtime.Close(context.Background())
}

// callJSON marshals the ABI call: writes `in` into guest memory, invokes
// fn, reads the NUL-terminated JSON result.
func (p *Plugin) callJSON(ctx context.Context, fn api.Function, in []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	allocRes, err := p.allocFn.Call(ctx, uint64(len(in)))
	if err != nil {
		return nil, fmt.Errorf("plugin %s: alloc: %w", p.name, err)
	}
	inPtr := uint32(allocRes[0])
	mem := p.mod.Memory()
	if !mem.Write(inPtr, in) {
		return nil, fmt.Errorf("plugin %s: write input out of bounds", p.name)
	}
	defer p.freeFn.Call(ctx, uint64(inPtr), uint64(len(in)))

	outRes, err := fn.Call(ctx, uint64(inPtr), uint64(len(in)))
	if err != nil {
		return nil, fmt.Errorf("plugin %s: hook call: %w", p.name, err)
	}
	outPtr := uint32(outRes[0])
	if outPtr == 0 {
		return nil, fmt.Errorf("plugin %s: hook returned null", p.name)
	}

	// Read the NUL-terminated output (bounded).
	size := mem.Size()
	max := maxOutputBytes
	if uint32(max) > size-outPtr {
		max = int(size - outPtr)
	}
	buf, ok := mem.Read(outPtr, uint32(max))
	if !ok {
		return nil, fmt.Errorf("plugin %s: read output out of bounds", p.name)
	}
	end := 0
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	return buf[:end], nil
}

// CallVerdict invokes verdict_hook (raw JSON in/out).
func (p *Plugin) CallVerdict(ctx context.Context, in []byte) ([]byte, error) {
	return p.callJSON(ctx, p.verdictFn, in)
}

// CallRedact invokes redact_hook (raw JSON in/out).
func (p *Plugin) CallRedact(ctx context.Context, in []byte) ([]byte, error) {
	if p.redactFn == nil {
		return nil, fmt.Errorf("plugin %s: no redact_hook", p.name)
	}
	return p.callJSON(ctx, p.redactFn, in)
}
