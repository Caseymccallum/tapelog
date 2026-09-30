package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/report"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
)

// newTestCmd runs behavioral regression scenarios against recorded
// cassettes: `tapelog test <scenario.yaml | dir>...`.
func newTestCmd() *cobra.Command {
	var plain bool
	var junitPath string
	var annotate bool
	cmd := &cobra.Command{
		Use:   "test [scenario.yaml | directory]...",
		Short: "Run behavioral regression scenarios against recorded cassettes",
		Long: `Run behavioral regression scenarios against recorded cassettes.

Each scenario asserts on the tool-call trajectory of a recorded session:
which tools were called (and how often), in what order, what the results
contained, which verdicts the policy produced — all deterministic, no LLM
judge needed. Exits non-zero on any failure: CI-native.

  tapelog test agent-tests/                 # every *.yaml in the directory
  tapelog test agent-tests/release.yaml     # one scenario
  tapelog test --plain agent-tests/         # flat output for CI logs`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := expandScenarios(args)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no scenario files found in %v", args)
			}

			failed := 0
			var results []report.Result
			for _, file := range files {
				sc, err := scenario.Load(file)
				if err != nil {
					return err
				}
				tape, err := replay.Load(scenario.ResolveFixture(sc, file))
				if err != nil {
					return fmt.Errorf("scenario %s: %w", file, err)
				}
				var evaluator policy.Evaluator
				if sc.Policy != "" {
					pol, err := policy.Load(resolveAgainst(sc.Policy, file))
					if err != nil {
						return fmt.Errorf("scenario %s: %w", file, err)
					}
					evaluator = pol
				}

				fails := scenario.Run(sc, tape, evaluator)
				name := sc.Name
				if name == "" {
					name = filepath.Base(file)
				}
				res := report.Result{Name: name, File: file, Asserts: len(sc.Assert)}
				for _, f := range fails {
					res.Failures = append(res.Failures, report.Failure{Check: f.Check, Detail: f.Detail})
				}
				results = append(results, res)
				if len(fails) == 0 {
					if plain {
						fmt.Printf("ok\t%s\n", name)
					} else {
						fmt.Printf("✓ %s (%d assertions)\n", name, len(sc.Assert))
					}
					continue
				}
				failed++
				if plain {
					fmt.Printf("FAIL\t%s\n", name)
				} else {
					fmt.Printf("✗ %s\n", name)
				}
				for _, f := range fails {
					fmt.Printf("    %s: %s\n", f.Check, f.Detail)
				}
			}
			if junitPath != "" {
				f, err := os.Create(junitPath)
				if err != nil {
					return err
				}
				err = report.WriteJUnit(f, results)
				cerr := f.Close()
				if err != nil {
					return err
				}
				if cerr != nil {
					return cerr
				}
			}
			// GitHub Actions annotations: opt-in via --annotate or
			// automatic inside a workflow run.
			if annotate || os.Getenv("GITHUB_ACTIONS") == "true" {
				for _, line := range report.GitHubAnnotations(results) {
					fmt.Println(line)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d scenario(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "flat output for CI logs")
	cmd.Flags().StringVar(&junitPath, "junit", "", "write a JUnit XML report to this path")
	cmd.Flags().BoolVar(&annotate, "annotate", false, "emit GitHub Actions ::error annotations (automatic on GITHUB_ACTIONS)")
	return cmd
}

// expandScenarios turns args into a list of scenario files (directories
// contribute their *.yaml / *.yml files, sorted).
func expandScenarios(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, a)
			continue
		}
		entries, err := filepath.Glob(filepath.Join(a, "*.y*ml"))
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	sortStrings(out)
	return out, nil
}

// resolveAgainst makes a path absolute against the scenario file's dir.
func resolveAgainst(path, scenarioPath string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(path, ".") || !filepath.IsAbs(path) {
		return filepath.Join(filepath.Dir(scenarioPath), path)
	}
	return path
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
