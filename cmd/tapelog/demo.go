package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/compat"
)

// The demo exists because a downloadable package cannot run `go run ./examples/...`:
// the first-run experience has to work with nothing but the binary. It drives the
// real recorder against the same fictional MCP fixture the tests use, so what a new
// user sees is the product's actual output, not a simulation of it.

const demoPolicy = `version: 1
default: allow
rules:
  - id: no-writes
    tool: ["write_file", "delete_file"]
    action: deny
    reason: the demonstration forbids writes
`

// The requests the demonstration sends, one JSON-RPC message per line. The deny and
// the tool error are deliberate: a demonstration that only shows successes teaches
// the wrong thing about what the boundary is for.
var demoRequests = []string{
	`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"tapelog-demo","version":"1"}}}`,
	`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/fictional/alpha.md"}}}`,
	`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_file","arguments":{"path":"/fictional/alpha.md","content":"denied on purpose"}}}`,
	`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
}

func newDemoCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run the offline demonstration: record, deny, verify, then show what to explore",
		Long: `Runs a complete fictional session with no network, no account and no source
checkout: a scripted MCP client drives a fictional server through the real boundary.

You should see one permitted call, one policy denial and one recorded tool error.
The recording is left on disk so you can inspect it, browse it in the workspace and
run the bundled Regression Lab suite against it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runDemo(dir) },
	}
	cmd.Flags().StringVar(&dir, "dir", "tapelog-demo", "directory to create the demonstration in")
	return cmd
}

// newDemoServeCmd is the fictional MCP server the demo records. Hidden: it is an
// implementation detail of `tapelog demo`, not a product surface.
func newDemoServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "demo-serve",
		Hidden: true,
		Short:  "Serve the fictional MCP fixture on stdio (used by `tapelog demo`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return serveDemoFixture()
		},
	}
}

func runDemo(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(abs)
	if err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty; choose another --dir or remove it", abs)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	recordings := filepath.Join(abs, "recordings")
	if err := os.MkdirAll(recordings, 0o700); err != nil {
		return err
	}
	policyPath := filepath.Join(abs, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(demoPolicy), 0o600); err != nil {
		return err
	}
	requestsPath := filepath.Join(abs, "requests.jsonl")
	if err := os.WriteFile(requestsPath, []byte(strings.Join(demoRequests, "\n")+"\n"), 0o600); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate this executable: %w", err)
	}

	fmt.Printf("tapelog demo — recording a fictional session into %s\n\n", recordings)
	rec := exec.Command(self, "record",
		"--harness", "tapelog-demo",
		"--policy", policyPath,
		"--recordings-dir", recordings,
		"--", self, "demo-serve")
	rec.Stdin = strings.NewReader(strings.Join(demoRequests, "\n") + "\n")
	rec.Stdout = os.Stdout
	rec.Stderr = os.Stderr
	if err := rec.Run(); err != nil {
		return fmt.Errorf("the demonstration session failed: %w", err)
	}

	logs, err := filepath.Glob(filepath.Join(recordings, "*.jsonl"))
	if err != nil || len(logs) == 0 {
		return fmt.Errorf("no recording was written under %s", recordings)
	}
	log := logs[0]
	fmt.Printf("\nEvidence written: %s\n", log)

	verify := exec.Command(self, "verify", log)
	verify.Stdout = os.Stdout
	verify.Stderr = os.Stderr
	if err := verify.Run(); err != nil {
		// verify's non-zero exit is a verdict, not a demo failure: show it and carry on.
		fmt.Fprintf(os.Stderr, "\n(verify exited non-zero — read the verdict above)\n")
	}

	fmt.Printf(`
What to look at next:

  %s inspect %s
      the call timeline: the allowed read, the deny with its rule, the tool error

  %s web --dir %s
      the same evidence in the workspace; open #session=%s

%s

The recording is a real one. Redaction ran before it was written: see
`+"`tapelog --help`"+` and docs/SECURITY-AND-LIMITATIONS.md for what it does and does not reach.
`, self, log, self, recordings, filepath.Base(log), labHint(self))
	return nil
}

// labHint points at the bundled suite when it can be found — next to the binary in
// an unpacked release archive, or under the current directory in a source checkout.
// Saying nothing is better than printing a path that does not exist.
func labHint(self string) string {
	candidates := []string{
		filepath.Join(filepath.Dir(self), "examples", "regression-demo"),
		filepath.Join("examples", "regression-demo"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "demo-suite.json")); err == nil {
			return fmt.Sprintf("  %s lab suite --dir %s --suite demo-suite.json\n"+
				"      the bundled fictional Regression Lab suite: 2 pass, 1 fail, 2 inconclusive by design\n"+
				"      (a FAIL and an INCONCLUSIVE are results, not errors — each says why)", self, dir)
		}
	}
	return "  (the bundled Regression Lab suite is not next to this binary; see docs/REGRESSION-LAB.md)"
}

func serveDemoFixture() error {
	// The same fictional fixture the compatibility tests use: fixed tool catalog,
	// canned results, one deliberate error. Nothing touches the filesystem or the network.
	return compat.NewServer(compat.Era2025).ServeStdio(os.Stdin, os.Stdout)
}
