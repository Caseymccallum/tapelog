package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/sandbox"
)

// newSandboxExecCmd is the internal re-exec entry point: apply OS sandbox
// restrictions to THIS process, then replace it with the real server.
// Used by `record`/`mux` when --sandbox-* flags are set (see
// internal/sandbox).
func newSandboxExecCmd() *cobra.Command {
	var (
		ro, rw  []string
		lenient bool
	)
	cmd := &cobra.Command{
		Use:    "__sandbox_exec [flags] -- <command> [args...]",
		Short:  "Internal: exec a command under OS sandbox restrictions",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 || dash >= len(args) {
				return fmt.Errorf("separate the command with --")
			}
			opts := sandbox.Options{ReadOnly: ro, ReadWrite: rw, Lenient: lenient}
			if err := opts.Restrict(); err != nil {
				return err
			}
			if !sandbox.Available() {
				fmt.Fprintln(os.Stderr, "tapelog: sandbox unsupported on this OS — running UNSANDBOXED")
			}
			return execPlatform(args[dash:])
		},
	}
	cmd.Flags().StringArrayVar(&ro, "ro", nil, "read-only path")
	cmd.Flags().StringArrayVar(&rw, "rw", nil, "read-write path")
	cmd.Flags().BoolVar(&lenient, "lenient", false, "degrade to unsandboxed instead of failing")
	return cmd
}
