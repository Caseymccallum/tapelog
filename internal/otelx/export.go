// Package otelx exports session logs as OpenTelemetry spans following the
// GenAI semantic conventions (span name `execute_tool <tool>`,
// attributes `gen_ai.operation.name` / `gen_ai.tool.name`), so tapelog
// sessions appear in any OTel-compatible backend.
package otelx

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/Caseymccallum/tapelog/internal/session"
)

const tsLayout = "2006-01-02T15:04:05.000Z"

// Export converts a session log into a trace (one root span + one span per
// tool call) and exports it. Empty endpoint => print to stdout.
func Export(ctx context.Context, path, endpoint string) error {
	events, err := readEvents(path)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("no events in %s", path)
	}

	var exp sdktrace.SpanExporter
	if endpoint != "" {
		exp, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	} else {
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	if err != nil {
		return fmt.Errorf("create exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes("",
			attribute.String("service.name", "tapelog"),
		)),
	)
	defer func() { _ = tp.Shutdown(ctx) }()
	tracer := tp.Tracer("tapelog")

	// Index events for pairing by JSON-RPC id.
	decisions := map[string]session.Event{}
	results := map[string]session.Event{}
	sessionID := events[0].SessionID
	start, _ := time.Parse(tsLayout, events[0].TS)
	end := start
	var calls []session.Event
	for _, e := range events {
		t, _ := time.Parse(tsLayout, e.TS)
		if t.After(end) {
			end = t
		}
		switch e.Type {
		case session.EventToolCall:
			calls = append(calls, e)
		case session.EventPolicyDecision:
			var p session.PolicyDecisionPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				decisions[string(p.ID)] = e
			}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				results[string(p.ID)] = e
			}
		}
	}

	rootCtx, root := tracer.Start(ctx, "session "+sessionID, trace.WithTimestamp(start))
	for _, e := range calls {
		var p session.ToolCallPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		callStart, _ := time.Parse(tsLayout, e.TS)
		attrs := []attribute.KeyValue{
			attribute.String("gen_ai.operation.name", "execute_tool"),
			attribute.String("gen_ai.tool.name", p.Tool),
			attribute.String("tapelog.session_id", sessionID),
			attribute.Int64("tapelog.seq", int64(e.Seq)),
		}
		if p.ToolDescriptorHash != "" {
			attrs = append(attrs, attribute.String("tapelog.tool_descriptor_hash", p.ToolDescriptorHash))
		}
		if p.DescriptorDrift {
			attrs = append(attrs, attribute.Bool("tapelog.descriptor_drift", true))
		}
		callEnd := callStart
		if dec, ok := decisions[string(p.ID)]; ok {
			var dp session.PolicyDecisionPayload
			_ = json.Unmarshal(dec.Payload, &dp)
			attrs = append(attrs,
				attribute.String("tapelog.verdict", dp.Verdict),
				attribute.String("tapelog.rule_id", dp.RuleID),
			)
			if t, _ := time.Parse(tsLayout, dec.TS); t.After(callEnd) {
				callEnd = t
			}
		}
		_, span := tracer.Start(rootCtx, "execute_tool "+p.Tool, trace.WithTimestamp(callStart), trace.WithAttributes(attrs...))
		if res, ok := results[string(p.ID)]; ok {
			if t, _ := time.Parse(tsLayout, res.TS); t.After(callEnd) {
				callEnd = t
			}
			var rp session.ToolResultPayload
			if json.Unmarshal(res.Payload, &rp) == nil && rp.IsError {
				span.SetStatus(codes.Error, "tool returned an error")
				span.SetAttributes(attribute.Bool("tapelog.tool_error", true))
			}
		} else {
			span.SetStatus(codes.Error, "denied or unanswered by policy")
			span.SetAttributes(attribute.Bool("tapelog.unanswered", true))
		}
		span.End(trace.WithTimestamp(callEnd))
	}
	root.End(trace.WithTimestamp(end))
	return nil
}

// readEvents parses the JSONL log.
func readEvents(path string) ([]session.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []session.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e session.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			events = append(events, e)
		}
	}
	return events, sc.Err()
}
