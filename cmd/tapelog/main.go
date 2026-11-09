// Command tapelog is the flight recorder and deterministic replay for
// AI agents: it records MCP tool traffic, enforces tool-call policy at the
// boundary, and verifies the tamper-evident session log.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/buildinfo"
)

// version is stamped by goreleaser at release time (via buildinfo).
var version = buildinfo.Version

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

	root.AddCommand(newDemoCmd())
	root.AddCommand(newDemoServeCmd())
	root.AddCommand(newRecordCmd())
	root.AddCommand(newMuxCmd())
	root.AddCommand(newQueueCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newFuzzCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newSetupCmd())
	root.AddCommand(newWebCmd())
	root.AddCommand(newBehaviourCmd())
	root.AddCommand(newLabCmd())
	root.AddCommand(newSandboxExecCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newCheckpointCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newPolicyCmd())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the tapelog version",
		Run: func(cmd *cobra.Command, args []string) {
			// Print the commit when the build carried one: the version string alone
			// cannot distinguish two source builds of the same line.
			if buildinfo.Commit != "" {
				fmt.Println("tapelog", version, "("+buildinfo.Commit+")")
				return
			}
			fmt.Println("tapelog", version)
		},
	})

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if errors.Is(err, errIncompleteBehaviour) || errors.Is(err, errLabInconclusive) {
			os.Exit(2)
		}
		// Structure verdicts: 3 = --require-structure / integrity check failed,
		// 4 = structure invalid, 5 = incomplete (boundary not observed).
		if errors.Is(err, errIntegrityFailed) || errors.Is(err, errStructureRequired) {
			os.Exit(3)
		}
		if errors.Is(err, errStructureFailed) {
			os.Exit(4)
		}
		if errors.Is(err, errIncompleteStructure) {
			os.Exit(5)
		}
		os.Exit(1)
	}
}
