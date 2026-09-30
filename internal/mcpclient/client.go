// Package mcpclient is a minimal MCP client over any transport.Transport:
// initialize handshake, tools/list, tools/call. Requests are correlated by
// JSON-RPC id; a read loop dispatches responses to waiters.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/transport"
)

// ProtocolVersion is the MCP protocol version we speak.
const ProtocolVersion = "2026-07-28"

// Client is a minimal MCP client. Safe for concurrent use.
type Client struct {
	t       transport.Transport
	nextID  int64
	mu      sync.Mutex
	pending map[string]chan *jsonrpc.Message
	closed  bool
}

// New wraps a transport and starts the response read loop.
func New(t transport.Transport) *Client {
	c := &Client{t: t, pending: map[string]chan *jsonrpc.Message{}}
	go c.readLoop()
	return c
}

func (c *Client) readLoop() {
	for {
		msg, err := c.t.Receive()
		if err != nil {
			c.failAll(err)
			return
		}
		if !msg.IsResponse() {
			continue // notifications/server-requests are out of scope (ADR 0004)
		}
		c.mu.Lock()
		ch := c.pending[string(msg.ID)]
		delete(c.pending, string(msg.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *Client) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}

// Request sends a JSON-RPC request and waits for its response.
func (c *Client) Request(ctx context.Context, method string, params any) (*jsonrpc.Message, error) {
	idNum := atomic.AddInt64(&c.nextID, 1)
	id := json.RawMessage(fmt.Sprintf("%d", idNum))

	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	ch := make(chan *jsonrpc.Message, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	c.pending[string(id)] = ch
	c.mu.Unlock()

	msg := &jsonrpc.Message{JSONRPC: "2.0", ID: id, Method: method, Params: paramsRaw}
	if err := c.t.Send(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return nil, fmt.Errorf("transport closed while waiting for %s", method)
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	paramsRaw, _ := json.Marshal(params)
	return c.t.Send(&jsonrpc.Message{JSONRPC: "2.0", Method: method, Params: paramsRaw})
}

// Initialize performs the MCP handshake and returns the server's name.
func (c *Client) Initialize(ctx context.Context, clientName string) (string, error) {
	resp, err := c.Request(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": "0.1.0"},
	})
	if err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("initialize: server error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	// Best-effort server name extraction; tolerated if absent.
	var result struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(resp.Result, &result)
	// Older servers expect this notification after the handshake; newer
	// (stateless 2026-07-28) servers ignore it.
	_ = c.Notify("notifications/initialized", map[string]any{})
	return result.ServerInfo.Name, nil
}

// ListTools returns the raw tool descriptors.
func (c *Client) ListTools(ctx context.Context) ([]json.RawMessage, error) {
	resp, err := c.Request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list: %s", resp.Error.Message)
	}
	var result struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes a tool. isError reflects a tool-level error result; the
// raw result (or error object) is returned for logging.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (raw json.RawMessage, isError bool, err error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	resp, err := c.Request(ctx, "tools/call", map[string]any{"name": name, "arguments": json.RawMessage(args)})
	if err != nil {
		return nil, false, err
	}
	if resp.Error != nil {
		eobj, _ := json.Marshal(resp.Error)
		return eobj, true, nil
	}
	return resp.Result, false, nil
}

// Close shuts down the transport.
func (c *Client) Close() error {
	c.failAll(fmt.Errorf("client closed"))
	return c.t.Close()
}
