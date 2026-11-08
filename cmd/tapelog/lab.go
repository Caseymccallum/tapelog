package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/lab"
	"github.com/Caseymccallum/tapelog/internal/web"
	"github.com/spf13/cobra"
)

var errLabInconclusive = errors.New("regression evidence is inconclusive")
var errLabFail = errors.New("one or more regression expectations failed")

func labStatus(status string) error {
	switch status {
	case "PASS":
		return nil
	case "FAIL":
		return errLabFail
	default:
		return errLabInconclusive
	}
}
func newLabCmd() *cobra.Command {
	root := &cobra.Command{Use: "lab", Short: "Run saved regression cases and suites on recorded evidence"}
	var dir, file, candidate string
	var jsonMode bool
	run := &cobra.Command{Use: "run", Short: "Run one case without executing recorded tools", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if dir == "" || file == "" {
			return fmt.Errorf("--dir and --case are required")
		}
		raw, err := lab.ReadLeaf(dir, file, ".json", lab.MaxDefinition)
		if err != nil {
			return fmt.Errorf("cannot open case: %q", err.Error())
		}
		c, err := lab.ParseCase(raw)
		if err != nil {
			return fmt.Errorf("invalid case: %q", err.Error())
		}
		r, err := lab.RunCase(dir, c, candidate)
		if err != nil {
			return err
		}
		var comparison any
		comparisonStatus := "not_requested"
		if c.Reference != "" && c.Reference != r.Evidence {
			delta, e := web.BehaviourWorkflowCompare(dir, c.Reference, r.Evidence)
			if e == nil {
				comparison = delta.Result
				comparisonStatus = delta.Status
			} else {
				comparisonStatus = "unavailable"
			}
		}
		output := map[string]any{"schema_version": 1, "case": r, "comparison": comparison, "comparison_status": comparisonStatus}
		if jsonMode {
			e := json.NewEncoder(cmd.OutOrStdout())
			e.SetIndent("", "  ")
			if err = e.Encode(output); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%s · %s\nEvidence: %s\n", r.Status, safeTerminal(r.Title), safeTerminal(r.Evidence))
			for _, x := range r.Checks {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s · %s: %s\n", x.Status, safeTerminal(x.Kind), safeTerminal(x.Detail))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\nWorkbench: %s\n", r.Note, workbenchLink(r.Evidence))
			if c.Reference != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Reference comparison: %s (observation only)\n", comparisonStatus)
			}
		}
		return labStatus(r.Status)
	}}
	run.Flags().StringVar(&dir, "dir", "", "directory containing case and recordings")
	run.Flags().StringVar(&file, "case", "", "case JSON filename")
	run.Flags().StringVar(&candidate, "candidate", "", "optional candidate recording filename")
	run.Flags().BoolVar(&jsonMode, "json", false, "emit versioned JSON")
	root.AddCommand(run)
	var suiteDir, suiteName, suiteCandidate string
	var suiteJSON bool
	suite := &cobra.Command{Use: "suite", Short: "Run an explicitly selected suite", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if suiteDir == "" || suiteName == "" {
			return fmt.Errorf("--dir and --suite are required")
		}
		raw, err := lab.ReadLeaf(suiteDir, suiteName, ".json", lab.MaxDefinition)
		if err != nil {
			return fmt.Errorf("cannot open suite: %q", err.Error())
		}
		s, err := lab.ParseSuite(raw)
		if err != nil {
			return fmt.Errorf("invalid suite: %q", err.Error())
		}
		result, err := lab.RunSuiteCandidate(suiteDir, s, suiteCandidate)
		if err != nil {
			return err
		}
		if suiteJSON {
			e := json.NewEncoder(cmd.OutOrStdout())
			e.SetIndent("", "  ")
			if err = e.Encode(result); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%s · %s: %d pass, %d fail, %d inconclusive\n", result.Status, safeTerminal(result.Title), result.Pass, result.Fail, result.Inconclusive)
			for _, r := range result.Cases {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s · %s · %s\n    %s\n", r.Status, safeTerminal(r.Title), safeTerminal(r.Evidence), workbenchLink(r.Evidence))
			}
		}
		return labStatus(result.Status)
	}}
	suite.Flags().StringVar(&suiteDir, "dir", "", "directory containing suite, cases and recordings")
	suite.Flags().StringVar(&suiteName, "suite", "", "suite JSON filename")
	suite.Flags().StringVar(&suiteCandidate, "candidate", "", "optional candidate recording applied to each case")
	suite.Flags().BoolVar(&suiteJSON, "json", false, "emit versioned JSON")
	root.AddCommand(suite)
	return root
}
