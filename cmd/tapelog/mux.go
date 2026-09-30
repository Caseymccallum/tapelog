package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/mediator"
	"github.com/Caseymccallum/tapelog/internal/mux"
	"github.com/Caseymccallum/tapelog/internal/plugin"
	"github.com/Caseymccallum/tapelog/internal/sandbox"
	"github.com/Caseymccallum/tapelog/internal/schemafire"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func newMuxCmd() *cobra.Command {
	var (
		configPath   string
		policyPath   string
		logPath      string
		sessionID    string
		harness      string
		task         string
		autoConfirm  bool
		denyOnDrift  bool
		pluginPaths  []string
		sandboxRO    []string
		sandboxRW    []string
		sandboxLax   bool
		approvalListen  string
		approvalToken   string
		approvalTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "mux",
		Short: "Run one mediated boundary across multiple MCP servers",
		Long: `Reads a mux config (YAML: stdio commands and/or HTTP endpoints),
connects every upstream, and serves the aggregate as a single MCP server
on stdio. Tools are namespaced <server>__<tool>; every call is mediated
(policy, flows, confirm, drift) and recorded into one session log.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			cfg, err := mux.LoadConfig(configPath)
			if err != nil {
				return err
			}

			// OS sandbox (defense-in-depth): wrap every stdio upstream.
			sandboxOpts := sandbox.Options{ReadOnly: sandboxRO, ReadWrite: sandboxRW, Lenient: sandboxLax}
			if sandboxOpts.Enabled() {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				for i := range cfg.Servers {
					if len(cfg.Servers[i].Command) > 0 {
						cfg.Servers[i].Command = sandboxOpts.WrapCommand(exe, cfg.Servers[i].Command)
					}
				}
			}
			evaluator, policyID, policyHash, pol, err := loadEvaluator(policyPath)
			if err != nil {
				return err
			}
			limTracker, injScanner, injMode, valueStore, err := buildGuards(pol)
			if err != nil {
				return err
			}
			if sessionID == "" {
				sessionID = generateSessionID()
			}

			writer, err := session.NewWriter(logPath, sessionID)
			if err != nil {
				return err
			}
			defer writer.Close()

			if _, err := writer.Append(session.EventSessionStart, session.SessionStartPayload{
				Harness: harness, PolicyID: policyID, PolicyHash: policyHash,
			}); err != nil {
				return err
			}

			confirmer, nonInteractive, shutdown, err := buildConfirmer(autoConfirm, approvalListen, approvalToken, approvalTimeout, logPath)
			if err != nil {
				return err
			}
			defer shutdown()
			plugins, err := plugin.NewChain(cmd.Context(), pluginPaths)
			if err != nil {
				return err
			}
			defer plugins.Close()

			med := mediator.New(mediator.Options{
				SessionID:      sessionID,
				Task:           task,
				Evaluator:      evaluator,
				Confirmer:      confirmer,
				Plugins:        plugins,
				Limits:         limTracker,
				Schemas:        schemafire.New(),
				Injection:      injScanner,
				InjectionMode:  injMode,
				Values:         valueStore,
				NonInteractive: nonInteractive,
				DenyOnDrift:    denyOnDrift,
				Writer:         writer,
			})

			m, err := mux.New(cmd.Context(), cfg, med)
			if err != nil {
				return err
			}
			defer m.Close()

			fmt.Fprintf(os.Stderr, "tapelog: mux session %s -> %s (%d servers, policy: %s)\n",
				sessionID, logPath, len(cfg.Servers), policyID)
			runErr := m.Serve(cmd.Context(), os.Stdin, os.Stdout)
			_, _ = writer.Append(session.EventSessionEnd, session.SessionEndPayload{Reason: "client disconnect"})
			return runErr
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "mux config YAML (required)")
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML file (omit for observe-only recording)")
	cmd.Flags().StringVar(&logPath, "log", "session.jsonl", "session log output path")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session id (generated if omitted)")
	cmd.Flags().StringVar(&harness, "harness", "unknown", "name of the agent harness being proxied")
	cmd.Flags().StringVar(&task, "task", "", "task label (enables task-scoped policy rules)")
	cmd.Flags().BoolVar(&autoConfirm, "auto-confirm", false, "treat confirm verdicts as allow (recorded)")
	cmd.Flags().BoolVar(&denyOnDrift, "deny-on-drift", false, "deny tool calls whose descriptor changed since first seen")
	cmd.Flags().StringArrayVar(&pluginPaths, "plugin", nil, "WASM plugin path (repeatable; see docs/PLUGINS.md)")
	cmd.Flags().StringArrayVar(&sandboxRO, "sandbox-ro", nil, "sandbox stdio upstreams: allow read-only access to this path (repeatable; Linux/landlock)")
	cmd.Flags().StringArrayVar(&sandboxRW, "sandbox-rw", nil, "sandbox stdio upstreams: allow read-write access to this path (repeatable; Linux/landlock)")
	cmd.Flags().BoolVar(&sandboxLax, "sandbox-lenient", false, "degrade to unsandboxed with a warning instead of failing")
	cmd.Flags().StringVar(&approvalListen, "approval-listen", "", "park confirm verdicts on a local approval queue at this address (e.g. 127.0.0.1:8923)")
	cmd.Flags().StringVar(&approvalToken, "approval-token", "", "require Authorization: Bearer <token> on the approval queue API")
	cmd.Flags().DurationVar(&approvalTimeout, "approval-timeout", 5*time.Minute, "how long a parked approval waits before failing closed (deny)")
	return cmd
}
