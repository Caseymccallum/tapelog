package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tapelog-dev/tapelog/internal/jsonrpc"
)

// HTTP is a minimal MCP streamable-HTTP client transport: each message is
// POSTed to the endpoint; responses arrive as a single JSON document or an
// SSE stream. Server-initiated messages (GET SSE channel) are not
// supported in v1 — documented in ADR 0004.
type HTTP struct {
	url     string
	headers map[string]string
	client  *http.Client

	mu    sync.Mutex
	queue []*jsonrpc.Message
	cond  *sync.Cond
}

// NewHTTP creates a streamable-HTTP transport for the given endpoint.
func NewHTTP(url string, headers map[string]string) *HTTP {
	h := &HTTP{url: url, headers: headers, client: &http.Client{}}
	h.cond = sync.NewCond(&h.mu)
	return h
}

// Send POSTs the message and queues every response message.
func (h *HTTP) Send(msg *jsonrpc.Message) error {
	b, err := jsonrpc.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), httpRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", h.url, err)
	}
	defer resp.Body.Close()

	var msgs []*jsonrpc.Message
	switch {
	case strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"):
		msgs, err = parseSSE(resp.Body)
	default:
		msgs, err = parseJSONBody(resp.Body, resp.StatusCode)
	}
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.queue = append(h.queue, msgs...)
	h.cond.Broadcast()
	h.mu.Unlock()
	return nil
}

// Receive returns the next queued response message (blocking).
func (h *HTTP) Receive() (*jsonrpc.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.queue) == 0 {
		h.cond.Wait()
	}
	msg := h.queue[0]
	h.queue = h.queue[1:]
	return msg, nil
}

// Close is a no-op for HTTP.
func (h *HTTP) Close() error { return nil }

// httpRequestTimeout bounds one POST round-trip.
const httpRequestTimeout = 30 * time.Second

// parseJSONBody handles a single-JSON-document response (202 Accepted with
// empty body is valid for notifications).
func parseJSONBody(body io.Reader, status int) ([]*jsonrpc.Message, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		if status >= 400 {
			return nil, fmt.Errorf("HTTP %d with empty body", status)
		}
		return nil, nil
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(trimmed, 200))
	}
	msg, err := jsonrpc.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	return []*jsonrpc.Message{msg}, nil
}

// parseSSE extracts JSON-RPC messages from `data:` lines.
func parseSSE(body io.Reader) ([]*jsonrpc.Message, error) {
	var msgs []*jsonrpc.Message
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue // event:, id:, retry:, comments — not needed here
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var probe json.RawMessage
		if json.Unmarshal([]byte(payload), &probe) != nil {
			continue
		}
		if msg, err := jsonrpc.Parse([]byte(payload)); err == nil {
			msgs = append(msgs, msg)
		}
	}
	return msgs, sc.Err()
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
