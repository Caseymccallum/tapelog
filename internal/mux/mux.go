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
	"strings"

	"gopkg.in/yaml.v3"

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
}

// New connects and handshakes every upstream (fail-fast).
func New(ctx context.Context, cfg *Config, med *mediator.Mediator) (*Mux, error) {
	mx := &Mux{med: med, byName: map[string]*Upstream{}, version: "0.1.0"}
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
		if _, err := client.Initialize(ctx, "tapelog-mux"); err != nil {
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
