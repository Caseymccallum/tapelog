//go:build wasip1 && wasm

// argguard is an example cassette plugin (docs/PLUGINS.md).
//
// It demonstrates both hooks with intentionally simple rules:
//   - verdict_hook: deny calls whose arguments contain the marker
//     "forbidden"; tighten allow -> confirm for tools containing "exec".
//   - redact_hook: mask "ACME-"-prefixed tokens (an org-specific secret
//     format the host's built-in patterns don't know).
//
// Build: ./build.ps1  (GOOS=wasip1 GOARCH=wasm go build -o argguard.wasm .)
package main

import (
	"encoding/json"
	"unsafe"
)

func main() {} // reactor-style module: exports are called by the host

// kept buffers keep Go's GC from collecting host-visible memory.
var kept [][]byte

func keep(b []byte) int32 {
	kept = append(kept, b)
	return int32(uintptr(unsafe.Pointer(&b[0])))
}

//go:wasmexport alloc
func alloc(size int32) int32 {
	return keep(make([]byte, size))
}

//go:wasmexport free
func free(ptr int32, size int32) {
	// Memory is GC-managed; nothing to do. (Kept buffers are bounded in
	// real plugins; this example never frees.)
}

// inBytes reads the host-provided input buffer.
func inBytes(ptr, length int32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

// outJSON marshals v as NUL-terminated JSON and returns its pointer.
func outJSON(v any) int32 {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{}`)
	}
	return keep(append(b, 0))
}

//go:wasmexport verdict_hook
func verdictHook(inPtr, inLen int32) int32 {
	var req struct {
		Tool    string          `json:"tool"`
		Args    json.RawMessage `json:"args"`
		Verdict string          `json:"verdict"`
	}
	_ = json.Unmarshal(inBytes(inPtr, inLen), &req)

	if bytesContains(req.Args, []byte("forbidden")) {
		return outJSON(map[string]string{
			"verdict": "deny",
			"reason":  "argguard: forbidden marker in arguments",
		})
	}
	if req.Verdict == "allow" && bytesContains([]byte(req.Tool), []byte("exec")) {
		return outJSON(map[string]string{
			"verdict": "confirm",
			"reason":  "argguard: exec tools require confirmation",
		})
	}
	return outJSON(map[string]string{}) // unchanged
}

//go:wasmexport redact_hook
func redactHook(inPtr, inLen int32) int32 {
	var req struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(inBytes(inPtr, inLen), &req)
	return outJSON(map[string]string{"text": maskAcme(req.Text)})
}

// maskAcme replaces ACME-<token> runs with [REDACTED].
func maskAcme(s string) string {
	out := []byte(s)
	for i := 0; i < len(out); i++ {
		if i+5 <= len(out) && string(out[i:i+5]) == "ACME-" {
			j := i + 5
			for j < len(out) && isTokenChar(out[j]) {
				j++
			}
			redacted := append([]byte{}, out[:i]...)
			redacted = append(redacted, []byte("[REDACTED]")...)
			out = append(redacted, out[j:]...)
			i += len("[REDACTED]") - 1
		}
	}
	return string(out)
}

func isTokenChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func bytesContains(b, sub []byte) bool {
	if len(sub) == 0 || len(b) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}
