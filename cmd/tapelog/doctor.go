package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/plugin"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/sandbox"
)

// newDoctorCmd checks the local setup: policy files compile, plugins
// load, platform capabilities are present. Exit non-zero on failures —
// run it before filing an issue (and in CI for your policy repo).
func newDoctorCmd() *cobra.Command {
	var (
		policyPath  string
		pluginPaths []string
		logPath     string
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the local setup (policy, plugins, platform)",
		Long: `Check the local setup and report what is ready:

  tapelog doctor --policy prod.yaml --plugin argguard.wasm
  tapelog doctor --policy packs/starter.yaml --log /var/log/tapelog/s.jsonl

Checks: policy compiles (rules, Cedar conditions, limits), WASM plugins
load, the OS sandbox is available, a terminal exists for interactive
approval, and the log location is writable. Exits non-zero on failures.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fails := 0
			report := func(status, name, detail string) {
				fmt.Printf("%s %s — %s\n", status, name, detail)
				if status == "✗" {
					fails++
				}
			}

			// 1. Runtime.
			report("✓", "runtime", fmt.Sprintf("tapelog %s, %s/%s", version, runtime.GOOS, runtime.GOARCH))

			// 2. Policy compiles (including Cedar conditions + limits).
			if policyPath == "" {
				report("!", "policy", "none given — record/mux will run observe-only (add --policy to enforce)")
			} else if p, err := policy.Load(policyPath); err != nil {
				report("✗", "policy", fmt.Sprintf("%s: %v", policyPath, err))
			} else {
				denies := 0
				for _, r := range p.Rules {
					if r.Action == "deny" {
						denies++
					}
				}
				report("✓", "policy", fmt.Sprintf("%s: %d rules (%d deny), %d flow(s), limits=%v",
					policyPath, len(p.Rules), denies, len(p.Flows), p.Limits.Enabled()))
			}

			// 3. Plugins load in the wazero sandbox.
			if len(pluginPaths) == 0 {
				report("!", "plugins", "none configured (optional)")
			} else {
				chain, err := plugin.NewChain(cmd.Context(), pluginPaths)
				if err != nil {
					report("✗", "plugins", err.Error())
				} else {
					chain.Close()
					report("✓", "plugins", fmt.Sprintf("%d plugin(s) loaded", len(pluginPaths)))
				}
			}

			// 4. Platform capabilities.
			if sandbox.Available() {
				report("✓", "os-sandbox", "Landlock available (--sandbox-ro/-rw usable)")
			} else {
				report("!", "os-sandbox", "no OS enforcement on this platform (Linux required); policy still enforces")
			}
			if approval.OpenTerminal() != nil {
				report("✓", "terminal", "interactive approval prompts available")
			} else {
				report("!", "terminal", "no controlling terminal — confirm verdicts need --auto-confirm or --approval-listen")
			}

			// 5. Log location writable.
			target := logPath
			if target == "" {
				target = "."
			}
			dir := target
			if filepath.Ext(dir) != "" {
				dir = filepath.Dir(dir)
			}
			if f, err := os.CreateTemp(dir, ".tapelog-doctor-*"); err != nil {
				report("✗", "log-location", fmt.Sprintf("%s is not writable: %v", dir, err))
			} else {
				f.Close()
				os.Remove(f.Name())
				report("✓", "log-location", fmt.Sprintf("%s is writable", dir))
			}

			// 6. Server tooling (informational).
			if _, err := exec.LookPath("node"); err == nil {
				report("✓", "node", "on PATH (for npx-based MCP servers)")
			} else {
				report("!", "node", "not on PATH — fine unless you run npx-based MCP servers")
			}

			if fails > 0 {
				return fmt.Errorf("%d check(s) failed", fails)
			}
			fmt.Println("all required checks passed")
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy file to validate")
	cmd.Flags().StringArrayVar(&pluginPaths, "plugin", nil, "WASM plugin to load-test (repeatable)")
	cmd.Flags().StringVar(&logPath, "log", "", "session log path to check for writability")
	return cmd
}
