// Package sandbox applies OS-level restrictions to the MCP server
// processes cassette spawns — defense-in-depth on top of policy (see
// docs/THREAT_MODEL.md). On Linux this uses Landlock (filesystem access:
// only listed paths are reachable). Other platforms have no enforcement
// yet; requesting a sandbox there is an error unless lenient mode is set.
//
// Model: restrictions are applied in a re-exec'd child
// (`cassette __sandbox_exec`) immediately before syscall.Exec of the real
// server, so cassette itself is never restricted and Landlock's
// inheritance does the rest. cgroup/seccomp limits are roadmap.
package sandbox

import (
	"errors"
	"fmt"
	"runtime"
)

// ErrUnsupported means this platform cannot enforce sandboxing.
var ErrUnsupported = errors.New("sandbox: no OS enforcement on this platform (landlock requires Linux)")

// Options describes filesystem restrictions for spawned servers.
type Options struct {
	ReadOnly  []string // readable but not writable
	ReadWrite []string // writable (implies readable)
	Lenient   bool     // degrade with a warning instead of failing
}

// Enabled reports whether any restriction is requested.
func (o Options) Enabled() bool {
	return len(o.ReadOnly) > 0 || len(o.ReadWrite) > 0
}

// Available reports whether this OS can enforce restrictions (Linux).
func Available() bool { return runtime.GOOS == "linux" }

// baselineRO paths keep binaries runnable (loader, libs, configs).
func baselineRO() []string {
	return []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"}
}

// baselineRW paths are writable by default (temp files).
func baselineRW() []string {
	return []string{"/tmp"}
}

// WrapCommand rewrites a server command so it executes under the
// restrictions via cassette's hidden __sandbox_exec subcommand.
func (o Options) WrapCommand(executable string, command []string) []string {
	wrapped := []string{executable, "__sandbox_exec"}
	if o.Lenient {
		wrapped = append(wrapped, "--lenient")
	}
	for _, p := range o.ReadOnly {
		wrapped = append(wrapped, "--ro", p)
	}
	for _, p := range o.ReadWrite {
		wrapped = append(wrapped, "--rw", p)
	}
	wrapped = append(wrapped, "--")
	return append(wrapped, command...)
}

// Restrict applies the restrictions to the current process. Callers
// should exec a new program image afterwards (restrictions are inherited).
func (o Options) Restrict() error {
	if runtime.GOOS != "linux" {
		if o.Lenient {
			return nil // caller warns
		}
		return fmt.Errorf("%w (use lenient mode to proceed unsandboxed)", ErrUnsupported)
	}
	return o.restrictPlatform()
}
