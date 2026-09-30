package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/spf13/cobra"
)

// newQueueCmd is the companion CLI for --approval-listen: review parked
// confirm verdicts and decide them from any terminal (or a script).
func newQueueCmd() *cobra.Command {
	var url, token string

	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Review and decide parked approvals (quarantine queue)",
		Long: `Review and decide the confirm verdicts parked by a tapelog record/mux
session running with --approval-listen.

  tapelog queue list
  tapelog queue allow 3 --note "reviewed the diff"
  tapelog queue deny 4`,
	}
	cmd.PersistentFlags().StringVar(&url, "url", "http://127.0.0.1:8923", "approval queue base URL")
	cmd.PersistentFlags().StringVar(&token, "token", "", "bearer token (must match --approval-token)")

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List parked approvals",
		RunE: func(cmd *cobra.Command, args []string) error {
			var body struct {
				Pending []struct {
					ID         int             `json:"id"`
					Tool       string          `json:"tool"`
					Args       json.RawMessage `json:"args"`
					AgeSeconds int             `json:"age_seconds"`
				} `json:"pending"`
			}
			if err := api(http.MethodGet, url+"/pending", token, nil, &body); err != nil {
				return err
			}
			if len(body.Pending) == 0 {
				fmt.Println("no parked approvals")
				return nil
			}
			for _, p := range body.Pending {
				fmt.Printf("#%-3d %8ds  %s  %s\n", p.ID, p.AgeSeconds, p.Tool, compactJSON(p.Args))
			}
			return nil
		},
	})

	decide := func(verdict string) *cobra.Command {
		var note string
		c := &cobra.Command{
			Use:   verdict + " <id>",
			Short: "Resolve a parked approval",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				id, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("id must be a number: %w", err)
				}
				req := map[string]any{"id": id, "verdict": verdict, "note": note}
				var resp map[string]any
				if err := api(http.MethodPost, url+"/decide", token, req, &resp); err != nil {
					return err
				}
				fmt.Printf("#%d -> %s\n", id, verdict)
				return nil
			},
		}
		c.Flags().StringVar(&note, "note", "", "note recorded with the decision")
		return c
	}
	cmd.AddCommand(decide("allow"))
	cmd.AddCommand(decide("allow_session"))
	cmd.AddCommand(decide("deny"))
	return cmd
}

// api performs one JSON call against the approval queue.
func api(method, endpoint, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, endpoint, resp.Status, compactJSON(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// compactJSON renders JSON single-line for CLI output.
func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	s := buf.String()
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
