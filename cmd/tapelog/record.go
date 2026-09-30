package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/mediator"
	"github.com/Caseymccallum/tapelog/internal/plugin"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/proxy"
	"github.com/Caseymccallum/tapelog/internal/sandbox"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func newRecordCmd() *cobra.Command {
	var (
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
	)
	cmd := &cobra.Command{
		Use:   "record [flags] -- <server command> [args...]",
		Short: "Record a session while proxying an MCP server",
		Long: `Spawns the given MCP server and proxies its stdio traffic for an agent
harness connected to tapelog's stdin/stdout. Every tool call is evaluated
against the policy (if given) and recorded into a hash-chained session log.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 || dash >= len(args) {
				return fmt.Errorf("separate the MCP server command with -- (example: tapelog record --log session.jsonl -- npx -y @modelcontextprotocol/server-filesystem .)")
			}
			serverCmd := args[dash:]

			// OS sandbox (defense-in-depth): run the server under Landlock
			// restrictions via the hidden re-exec wrapper.
			sandboxOpts := sandbox.Options{ReadOnly: sandboxRO, ReadWrite: sandboxRW, Lenient: sandboxLax}
			if sandboxOpts.Enabled() {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				serverCmd = sandboxOpts.WrapCommand(exe, serverCmd)
			}

			evaluator, policyID, policyHash, err := loadEvaluator(policyPath)
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

			confirmer, nonInteractive := buildConfirmer(autoConfirm)
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
				NonInteractive: nonInteractive,
				DenyOnDrift:    denyOnDrift,
				Writer:         writer,
			})
			tracker := newDescriptorTracker(med)

			server := exec.Command(serverCmd[0], serverCmd[1:]...)
			server.Stderr = os.Stderr
			serverStdin, err := server.StdinPipe() // requests INTO the server
			if err != nil {
				return err
			}
			serverStdout, err := server.StdoutPipe() // responses FROM the server
			if err != nil {
				return err
			}
			if err := server.Start(); err != nil {
				return fmt.Errorf("start MCP server %q: %w", serverCmd[0], err)
			}
			defer func() {
				_ = server.Process.Kill()
				_, _ = server.Process.Wait()
			}()

			hooks := proxy.Hooks{
				OnRawMessage: tracker.observe,
				OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (proxy.Decision, *proxy.DenyData) {
					tracker.awaitPendingListings(2 * time.Second) // evaluate against fresh pins
					out := med.Decide(id, call.Name, call.Arguments)
					if !out.Allowed {
						return proxy.DecisionDeny, &proxy.DenyData{
							Code: "tool_denied", RuleID: out.RuleID,
							Reason: out.Reason, Verdict: out.Verdict,
						}
					}
					return proxy.DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) {
					med.Result(id, isError, result)
				},
			}

			fmt.Fprintf(os.Stderr, "tapelog: session %s -> %s (policy: %s)\n", sessionID, logPath, policyID)
			runErr := proxy.Run(cmd.Context(), os.Stdin, os.Stdout, serverStdout, serverStdin, hooks)
			_, _ = writer.Append(session.EventSessionEnd, session.SessionEndPayload{Reason: "client disconnect"})
			return runErr
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML file (omit for observe-only recording)")
	cmd.Flags().StringVar(&logPath, "log", "session.jsonl", "session log output path")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session id (generated if omitted)")
	cmd.Flags().StringVar(&harness, "harness", "unknown", "name of the agent harness being proxied")
	cmd.Flags().StringVar(&task, "task", "", "task label (enables task-scoped policy rules)")
	cmd.Flags().BoolVar(&autoConfirm, "auto-confirm", false, "treat confirm verdicts as allow (recorded)")
	cmd.Flags().BoolVar(&denyOnDrift, "deny-on-drift", false, "deny tool calls whose descriptor changed since first seen")
	cmd.Flags().StringArrayVar(&pluginPaths, "plugin", nil, "WASM plugin path (repeatable; see docs/PLUGINS.md)")
	cmd.Flags().StringArrayVar(&sandboxRO, "sandbox-ro", nil, "sandbox the server: allow read-only access to this path (repeatable; Linux/landlock)")
	cmd.Flags().StringArrayVar(&sandboxRW, "sandbox-rw", nil, "sandbox the server: allow read-write access to this path (repeatable; Linux/landlock)")
	cmd.Flags().BoolVar(&sandboxLax, "sandbox-lenient", false, "degrade to unsandboxed with a warning instead of failing")
	return cmd
}

// loadEvaluator resolves the policy (or observe-only mode) and returns the
// policy id + file hash recorded in session/start.
func loadEvaluator(policyPath string) (policy.Evaluator, string, string, error) {
	if policyPath == "" {
		return policy.AllowAll{}, "observe", "", nil
	}
	p, err := policy.Load(policyPath)
	if err != nil {
		return nil, "", "", err
	}
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(data)
	return p, policyPath, hex.EncodeToString(sum[:]), nil
}

// buildConfirmer implements the confirm strategy: --auto-confirm allows
// (recorded); otherwise prompt on the terminal; with no terminal available,
// fail closed.
func buildConfirmer(autoConfirm bool) (approval.Confirmer, bool) {
	if autoConfirm {
		return approval.Auto{}, false
	}
	if tty := approval.OpenTerminal(); tty != nil {
		return approval.NewInteractive(tty, os.Stderr), false
	}
	return approval.Deny{}, true
}

// generateSessionID returns a timestamped random session id.
func generateSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405.000"), hex.EncodeToString(b))
}
