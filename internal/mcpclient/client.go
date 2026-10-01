// Package mcpclient is a minimal dual-era MCP client over any
// transport.Transport (spec 2026-07-28 §Versioning "backward compatibility"):
// modern servers are probed with `server/discover` and need no handshake;
// legacy servers fall back to the initialize/notifications/initialized
// exchange. Every request self-describes via `_meta` (protocol version,
// client capabilities, client info). Requests are correlated by JSON-RPC
// id; a read loop dispatches responses to waiters.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Caseymccallum/tapelog/internal/buildinfo"
	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/transport"
)

// ProtocolVersion is the MCP protocol version we speak.
const ProtocolVersion = transport.MCPVersion

// CodeUnsupportedProtocolVersion is returned by modern servers when they
// cannot serve the requested revision (spec 2026-07-28, error code -32022);
// data.supported lists what they can serve.
const CodeUnsupportedProtocolVersion = -32022

// Client is a minimal MCP client. Safe for concurrent use.
type Client struct {
	t         transport.Transport
	nextID    int64
	mu        sync.Mutex
	pending   map[string]chan *jsonrpc.Message
	closed    bool
	protocol  string // negotiated protocol version ("" until connected)
	clientName string // identity reported in `_meta`
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

// Request sends a JSON-RPC request and waits for its response. Every
// request self-describes via `_meta` (spec 2026-07-28: protocol version,
// client capabilities, client info on each request). A
// UnsupportedProtocolVersionError (-32022) triggers one retry on a
// mutually supported version.
func (c *Client) Request(ctx context.Context, method string, params any) (*jsonrpc.Message, error) {
	resp, err := c.roundTrip(ctx, method, c.withMeta(params))
	if err != nil {
		return nil, err
	}
	if resp.Error == nil || resp.Error.Code != CodeUnsupportedProtocolVersion {
		return resp, nil
	}
	pick, ok := mutualVersion(versionsFrom(resp.Error.Data))
	if !ok {
		return resp, nil // no mutual version: surface the server's error
	}
	c.setProtocol(pick)
	return c.roundTrip(ctx, method, c.withMeta(params))
}

// roundTrip sends one request and waits for its correlated response.
func (c *Client) roundTrip(ctx context.Context, method string, params any) (*jsonrpc.Message, error) {
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
	if err := c.t.Send(ctx, msg); err != nil {
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
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	paramsRaw, _ := json.Marshal(c.withMeta(params))
	return c.t.Send(ctx, &jsonrpc.Message{JSONRPC: "2.0", Method: method, Params: paramsRaw})
}

// withMeta injects the per-request `_meta` self-description into an
// object params map (spec 2026-07-28 §Basic/`_meta`). Caller-supplied
// `_meta` keys (e.g. traceparent) are preserved; the
// io.modelcontextprotocol/* identity keys are ours. Non-object params are
// returned untouched.
func (c *Client) withMeta(params any) any {
	raw, err := json.Marshal(params)
	if err != nil {
		return params
	}
	m := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &m); err != nil {
			return params
		}
	}
	meta := map[string]any{}
	if existing, ok := m["_meta"]; ok {
		_ = json.Unmarshal(existing, &meta)
	}
	meta["io.modelcontextprotocol/protocolVersion"] = c.currentProtocol()
	meta["io.modelcontextprotocol/clientCapabilities"] = map[string]any{}
	meta["io.modelcontextprotocol/clientInfo"] = map[string]any{"name": c.name(), "version": buildinfo.Version}
	metaRaw, _ := json.Marshal(meta)
	m["_meta"] = metaRaw
	return m
}

// setProtocol records the negotiated protocol version.
func (c *Client) setProtocol(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protocol = v
}

// currentProtocol returns the negotiated version, defaulting to ours.
func (c *Client) currentProtocol() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.protocol == "" {
		return ProtocolVersion
	}
	return c.protocol
}

// setName records the client identity reported in `_meta`.
func (c *Client) setName(n string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n != "" {
		c.clientName = n
	}
}

// name returns the recorded client identity.
func (c *Client) name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientName == "" {
		return "tapelog"
	}
	return c.clientName
}

// versionsFrom extracts data.supported from an
// UnsupportedProtocolVersionError payload.
func versionsFrom(data json.RawMessage) []string {
	var d struct {
		Supported []string `json:"supported"`
	}
	_ = json.Unmarshal(data, &d)
	return d.Supported
}

// mutualVersion picks the best version we can speak from a server's
// supported list: ours first, then the newest handshake-based revision.
func mutualVersion(supported []string) (string, bool) {
	has := func(v string) bool {
		for _, s := range supported {
			if s == v {
				return true
			}
		}
		return false
	}
	if has(ProtocolVersion) {
		return ProtocolVersion, true
	}
	for _, legacy := range transport.LegacyVersions {
		if has(legacy) {
			return legacy, true
		}
	}
	return "", false
}

// Connect performs the dual-era startup sequence (spec 2026-07-28
// "Backward Compatibility") and returns the server's name:
//
//  1. Probe `server/discover` — modern servers MUST implement it and need
//     no handshake at all.
//  2. On a non-modern answer (method-not-found, timeout, ...) fall back to
//     the legacy initialize / notifications/initialized exchange.
//
// The negotiated era is cached for the connection's lifetime.
func (c *Client) Connect(ctx context.Context, clientName string) (string, error) {
	c.setName(clientName)

	// Modern probe, bounded so a silent legacy server can't hang us. A
	// server that answers late is rescued below.
	probeCtx, cancel := context.WithTimeout(ctx, discoverProbeTimeout)
	resp, err := c.Request(probeCtx, "server/discover", map[string]any{})
	cancel()
	if err == nil {
		if name, modern, verr := parseDiscover(resp); modern {
			if verr != nil {
				return "", verr
			}
			return name, nil
		}
	}

	// Legacy era: initialize + notifications/initialized handshake.
	name, lerr := c.legacyInitialize(ctx, clientName)
	if lerr == nil {
		return name, nil
	}
	// Rescue a slow modern server: the probe may have timed out, and a
	// modern server rejects `initialize` — retry the probe uncapped.
	if resp, perr := c.Request(ctx, "server/discover", map[string]any{}); perr == nil {
		if name, modern, _ := parseDiscover(resp); modern {
			return name, nil
		}
	}
	return "", lerr
}

// discoverProbeTimeout bounds the modern-era probe before legacy fallback.
const discoverProbeTimeout = 5 * time.Second

// parseDiscover reads a server/discover result. modern is true when the
// response identifies a modern-era server (a DiscoverResult or a -32022);
// err (with modern=true) means modern but unserveable.
func parseDiscover(resp *jsonrpc.Message) (name string, modern bool, err error) {
	if resp.Error != nil {
		if resp.Error.Code == CodeUnsupportedProtocolVersion {
			return "", true, fmt.Errorf("server supports MCP versions %v; none mutually supported (tapelog speaks %s and the legacy handshake revisions)",
				versionsFrom(resp.Error.Data), ProtocolVersion)
		}
		return "", false, nil // non-modern error (e.g. method not found)
	}
	var d struct {
		SupportedVersions []string `json:"supportedVersions"`
		Meta              struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	if json.Unmarshal(resp.Result, &d) != nil || len(d.SupportedVersions) == 0 {
		return "", false, nil // not a DiscoverResult: treat as legacy
	}
	if _, ok := mutualVersion(d.SupportedVersions); !ok {
		return "", true, fmt.Errorf("server supports MCP versions %v; none mutually supported (tapelog speaks %s and the legacy handshake revisions)",
			d.SupportedVersions, ProtocolVersion)
	}
	return d.Meta.ServerInfo.Name, true, nil
}

// legacyInitialize performs the handshake-based startup used by servers
// on protocol revisions 2025-11-25 and earlier.
func (c *Client) legacyInitialize(ctx context.Context, clientName string) (string, error) {
	legacy := transport.LegacyVersions[0] // newest handshake-based revision
	resp, err := c.Request(ctx, "initialize", map[string]any{
		"protocolVersion": legacy,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": buildinfo.Version},
	})
	if err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("initialize: server error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	// Best-effort server name extraction; tolerated if absent.
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(resp.Result, &result)
	if result.ProtocolVersion != "" {
		c.setProtocol(result.ProtocolVersion)
	} else {
		c.setProtocol(legacy)
	}
	// Legacy servers expect this notification after the handshake; modern
	// (stateless 2026-07-28) servers ignore it.
	_ = c.Notify(ctx, "notifications/initialized", map[string]any{})
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
// CallTool invokes a tool by name and arguments.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (raw json.RawMessage, isError bool, err error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return c.CallToolParams(ctx, map[string]any{"name": name, "arguments": json.RawMessage(args)})
}

// CallToolParams invokes tools/call with caller-supplied params forwarded
// verbatim (plus our `_meta`). Use this when the client's original params
// carry fields beyond name/arguments — MRTR `inputResponses`, elicitation
// payloads, caller `_meta` — so nothing is silently dropped on the floor.
func (c *Client) CallToolParams(ctx context.Context, params any) (raw json.RawMessage, isError bool, err error) {
	resp, err := c.Request(ctx, "tools/call", params)
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
