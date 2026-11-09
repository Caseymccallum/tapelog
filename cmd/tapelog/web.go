package main

import (
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/web"
)

// newWebCmd serves the review dashboard over a directory of session
// logs: browse sessions, inspect decisions, without a live recording.
func newWebCmd() *cobra.Command {
	var (
		dir        string
		listen     string
		token      string
		policyPath string
	)
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Open the local session workbench over a directory of recordings",
		Long: `Serve the local Agent Session Workbench over a directory of recordings.

Search and filter sessions, investigate calls and policy decisions,
compare recorded responses, and preview/run reusable trajectory assertions.
Saving a test is an explicit browser action; recordings are never edited.
Replay guidance uses the existing hermetic CLI, never live tool execution.
Large files stay bounded (16 MiB / 50,000 events; use the CLI above this).
The workbench observes MCP-mediated traffic only. It cannot see built-in
agent operations or establish current client connectivity from a past log. The
same security posture as --approval-listen: loopback Host pinning, strict
CSP, optional bearer token. No live approvals in directory mode.

  tapelog web --dir ./sessions
  tapelog web --dir /var/log/tapelog --listen 127.0.0.1:8923 --token sekret
  tapelog web --dir ./sessions --policy ./tapelog.yaml   # explains the active rules`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}
			info, err := os.Stat(dir)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("--dir %s is not a directory", dir)
			}
			srv := &http.Server{Addr: listen, Handler: web.DirHandlerWithPolicy(dir, policyPath, token)}
			// Bind before announcing: a busy port used to print the dashboard URL
			// first and then fail, so the line a script greps for described a
			// server that was never running.
			ln, err := net.Listen("tcp", listen)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "tapelog: session dashboard on http://%s (%s)\n", ln.Addr().String(), dir)
			if policyPath != "" {
				fmt.Fprintf(os.Stderr, "tapelog: policy studio explains %s (read-only; nothing is written or activated)\n", policyPath)
			}
			return srv.Serve(ln)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "directory of session logs (*.jsonl)")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8923", "listen address (loopback by default)")
	cmd.Flags().StringVar(&token, "token", "", "require Authorization: Bearer <token>")
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML to explain in the Policy Studio (read-only; never modified)")
	return cmd
}
