package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/web"
)

// newWebCmd serves the review dashboard over a directory of session
// logs: browse sessions, inspect decisions, without a live recording.
func newWebCmd() *cobra.Command {
	var (
		dir     string
		listen  string
		token   string
	)
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Serve the review dashboard over a directory of session logs",
		Long: `Serve the review dashboard over a directory of session logs.

Browse recorded sessions in a browser: session list (event and denial
counts), per-session event timeline, policy decisions highlighted. The
same security posture as --approval-listen: loopback Host pinning, strict
CSP, optional bearer token. Read-only — no live approvals.

  tapelog web --dir ./sessions
  tapelog web --dir /var/log/tapelog --listen 127.0.0.1:8923 --token sekret`,
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
			srv := &http.Server{Addr: listen, Handler: web.DirHandler(dir, token)}
			fmt.Fprintf(os.Stderr, "tapelog: session dashboard on http://%s (%s)\n", listen, dir)
			return srv.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "directory of session logs (*.jsonl)")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8923", "listen address (loopback by default)")
	cmd.Flags().StringVar(&token, "token", "", "require Authorization: Bearer <token>")
	return cmd
}
