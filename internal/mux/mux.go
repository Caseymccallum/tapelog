// Package mux aggregates multiple MCP upstreams (stdio commands and
// streamable-HTTP endpoints) behind one mediated boundary: a single
// session log, one policy, one taint state. Tools are namespaced
// <server>__<tool> and every call goes through the mediator pipeline.
//
// v1 semantics (ADR 0004): tool calls are processed strictly in arrival
// order (deterministic taint); upstream listings are refreshed on every
// tools/list request so descriptor drift is detected live.
package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/Caseymccallum/tapelog/internal/buildinfo"
	"github.com/Caseymccallum/tapelog/internal/mcpclient"
	"github.com/Caseymccallum/tapelog/internal/mediator"
	"github.com/Caseymccallum/tapelog/internal/transport"
)

// ServerConfig describes one upstream MCP server: either a command
// (stdio) or a URL (streamable HTTP), never both.
type ServerConfig struct {
	Name    string            `yaml:"name"`
	Command []string          `yaml:"command"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

// Config is the mux configuration file.
type Config struct {
	Version int            `yaml:"version"`
	Servers []ServerConfig `yaml:"servers"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// LoadConfig reads and validates a mux config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mux config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mux config: %w", err)
	}
	if cfg.Version != 1 {
		return nil, fmt.Errorf("unsupported mux config version %d (want 1)", cfg.Version)
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("mux config has no servers")
	}
	seen := map[string]bool{}
	for _, s := range cfg.Servers {
		if !namePattern.MatchString(s.Name) {
			return nil, fmt.Errorf("server name %q must match %s (no underscores-only '__')", s.Name, namePattern)
		}
		if strings.Contains(s.Name, "__") {
			return nil, fmt.Errorf("server name %q must not contain '__' (namespace separator)", s.Name)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("duplicate server name %q", s.Name)
		}
		seen[s.Name] = true
		if (len(s.Command) == 0) == (s.URL == "") {
			return nil, fmt.Errorf("server %q: exactly one of command/url is required", s.Name)
		}
	}
	return &cfg, nil
}

// Upstream is one connected upstream server.
type Upstream struct {
	cfg    ServerConfig
	client *mcpclient.Client
}

// Mux routes namespaced calls to upstreams through one mediator.
type Mux struct {
	med     *mediator.Mediator
	byName  map[string]*Upstream
	version string

	mu        sync.Mutex
	resOwner  map[string]string // resource uri -> server (from resources/list)
	tmplOwner map[string]string // uriTemplate -> server
}

// New connects and handshakes every upstream (fail-fast).
func New(ctx context.Context, cfg *Config, med *mediator.Mediator) (*Mux, error) {
	mx := &Mux{med: med, byName: map[string]*Upstream{}, version: buildinfo.Version,
		resOwner: map[string]string{}, tmplOwner: map[string]string{}}
	for _, sc := range cfg.Servers {
		var t transport.Transport
		var err error
		if sc.URL != "" {
			t = transport.NewHTTP(sc.URL, sc.Headers)
		} else {
			t, err = transport.NewStdio(sc.Command)
		}
		if err != nil {
			return nil, fmt.Errorf("server %q: %w", sc.Name, err)
		}
		client := mcpclient.New(t)
		if _, err := client.Connect(ctx, "tapelog-mux"); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("server %q: %w", sc.Name, err)
		}
		mx.byName[sc.Name] = &Upstream{cfg: sc, client: client}
	}
	return mx, nil
}

// Close tears down all upstreams.
func (mx *Mux) Close() {
	for _, u := range mx.byName {
		_ = u.client.Close()
	}
}

// tools aggregates namespaced descriptors from every upstream, refreshing
// listings (which re-pins descriptors and detects drift). ok is false when
// any upstream fails — the mux fails closed.
// callFirst forwards a surface call (resources/read, prompts/get, ...) to
// upstreams in deterministic name order and returns the first successful
// response. MCP resource URIs and prompt names are not namespaced across
// servers, so first-success is the honest v0 routing (docs/POLICY.md).
func (mx *Mux) callFirst(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	names := make([]string, 0, len(mx.byName))
	for name := range mx.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	var lastErr error
	for _, name := range names {
		resp, err := mx.byName[name].client.Request(ctx, method, params)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", name, err)
			continue
		}
		if resp.Error != nil {
			lastErr = fmt.Errorf("%s: %s", name, resp.Error.Message)
			continue
		}
		return resp.Result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no upstream available")
	}
	return nil, lastErr
}

// callRouted forwards a surface call: catalog listings aggregate across
// upstreams, resource reads route to the catalog's owner (falling back to
// first-success), and namespaced prompt names route like tools.
func (mx *Mux) callRouted(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "resources/list", "prompts/list", "resources/templates/list":
		return mx.aggregate(ctx, method)
	case "resources/read":
		if uri := paramStr(params, "uri"); uri != "" {
			mx.mu.Lock()
			owner := mx.resOwner[uri]
			mx.mu.Unlock()
			if owner != "" {
				return mx.callServer(ctx, owner, method, params)
			}
		}
	case "prompts/get":
		if name := paramStr(params, "name"); name != "" {
			if server, tool, ok := mediator.SplitNamespacedName(name); ok {
				if np := withParam(params, "name", tool); np != nil {
					return mx.callServer(ctx, server, method, np)
				}
			}
		}
	}
	return mx.callFirst(ctx, method, params)
}

// aggregate merges one catalog listing from every upstream. Entries are
// annotated with their server; prompt names are namespaced <server>__<name>
// (unwrapped on prompts/get); resource URIs are left intact but their
// owner is remembered for precise routing.
func (mx *Mux) aggregate(ctx context.Context, method string) (json.RawMessage, error) {
	key := map[string]string{
		"resources/list":           "resources",
		"prompts/list":             "prompts",
		"resources/templates/list": "resourceTemplates",
	}[method]
	names := make([]string, 0, len(mx.byName))
	for name := range mx.byName {
		names = append(names, name)
	}
	sort.Strings(names)

	merged := []json.RawMessage{}
	for _, server := range names {
		resp, err := mx.byName[server].client.Request(ctx, method, map[string]any{})
		if err != nil || resp.Error != nil {
			continue // catalog merge is best-effort per upstream
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(resp.Result, &result) != nil {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(result[key], &items) != nil {
			continue
		}
		for _, item := range items {
			var entry map[string]any
			if json.Unmarshal(item, &entry) != nil {
				continue
			}
			entry["_tapelog_server"] = server
			switch method {
			case "prompts/list":
				if name, ok := entry["name"].(string); ok {
					entry["name"] = mediator.NamespacedName(server, name)
				}
			case "resources/list":
				if uri, ok := entry["uri"].(string); ok {
					mx.mu.Lock()
					mx.resOwner[uri] = server
					mx.mu.Unlock()
				}
			case "resources/templates/list":
				if uri, ok := entry["uriTemplate"].(string); ok {
					mx.mu.Lock()
					mx.tmplOwner[uri] = server
					mx.mu.Unlock()
				}
			}
			raw, _ := json.Marshal(entry)
			merged = append(merged, raw)
		}
	}
	raw, _ := json.Marshal(map[string]any{key: merged})
	return raw, nil
}

// callServer sends one surface call to a specific upstream.
func (mx *Mux) callServer(ctx context.Context, server, method string, params json.RawMessage) (json.RawMessage, error) {
	u := mx.byName[server]
	if u == nil {
		return mx.callFirst(ctx, method, params)
	}
	resp, err := u.client.Request(ctx, method, params)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", server, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", server, resp.Error.Message)
	}
	return resp.Result, nil
}

// paramStr extracts a string parameter from raw params.
func paramStr(params json.RawMessage, key string) string {
	var m map[string]any
	if json.Unmarshal(params, &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// withParam returns params with one string key replaced (nil on error).
func withParam(params json.RawMessage, key, value string) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(params, &m) != nil {
		return nil
	}
	m[key] = value
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return raw
}

func (mx *Mux) tools(ctx context.Context) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for name, u := range mx.byName {
		descs, err := u.client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("server %q: tools/list: %w", name, err)
		}
		for _, desc := range descs {
			named, err := renameTool(desc, name)
			if err != nil {
				continue
			}
			var probe struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(named, &probe)
			hash, _ := mx.med.HashDescriptor(named)
			mx.med.PinDescriptor(probe.Name, hash)
			_ = mx.med.PinSchema(probe.Name, named) // inbound schema firewall
			out = append(out, named)
		}
	}
	return out, nil
}

// renameTool returns the descriptor with its name namespaced.
func renameTool(desc json.RawMessage, server string) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(desc, &m); err != nil {
		return nil, err
	}
	var name string
	if err := json.Unmarshal(m["name"], &name); err != nil || name == "" {
		return nil, fmt.Errorf("descriptor without name")
	}
	newName, _ := json.Marshal(mediator.NamespacedName(server, name))
	m["name"] = newName
	return json.Marshal(m)
}
