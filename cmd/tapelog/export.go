package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/otelx"
)

func newExportCmd() *cobra.Command {
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export session logs to other formats",
	}

	var endpoint string
	otelCmd := &cobra.Command{
		Use:   "otel <session.jsonl>",
		Short: "Export a session as an OpenTelemetry trace",
		Long: `Emits one root span per session and one span per tool call, following
the GenAI semantic conventions (execute_tool / gen_ai.tool.name).

With --endpoint, exports via OTLP/HTTP to a collector (e.g.
http://localhost:4318/v1/traces); without it, prints spans to stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := otelx.Export(context.Background(), args[0], endpoint); err != nil {
				return err
			}
			if endpoint != "" {
				fmt.Printf("exported %s to %s\n", args[0], endpoint)
			}
			return nil
		},
	}
	otelCmd.Flags().StringVar(&endpoint, "endpoint", "", "OTLP/HTTP collector URL (default: stdout)")
	exportCmd.AddCommand(otelCmd)
	return exportCmd
}
