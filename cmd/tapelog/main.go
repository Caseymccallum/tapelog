// Command tapelog is the flight recorder and deterministic replay for
// AI agents: it records MCP tool traffic, enforces tool-call policy at the
// boundary, and verifies the tamper-evident session log.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is stamped by goreleaser at release time.
var version = "0.3.0-dev"

func main() {
	root := &cobra.Command{
		Use:   "tapelog",
		Short: "Flight recorder and deterministic replay for AI agents",
		Long: `tapelog sits between an agent harness and MCP tool servers.
It records every tool call into a tamper-evident session log, evaluates
allow/deny/confirm policy before side effects occur, and verifies logs.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newRecordCmd())
	root.AddCommand(newMuxCmd())
	root.AddCommand(newQueueCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newFuzzCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newWebCmd())
	root.AddCommand(newSandboxExecCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newPolicyCmd())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the tapelog version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("tapelog", version)
		},
	})

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
