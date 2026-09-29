package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/tapelog-dev/tapelog/internal/approval"
	"github.com/tapelog-dev/tapelog/internal/jsonrpc"
	"github.com/tapelog-dev/tapelog/internal/mcpclient"
	"github.com/tapelog-dev/tapelog/internal/mediator"
	"github.com/tapelog-dev/tapelog/internal/policy"
	"github.com/tapelog-dev/tapelog/internal/session"
)

// fakeTransport is a scripted in-memory MCP server.
type fakeTransport struct {
	mu    sync.Mutex
	cond  *sync.Cond
	queue []*jsonrpc.Message
	tools []json.RawMessage
	calls []string // tool names that were invoked
}

func newFakeTransport(tools ...string) *fakeTransport {
	f := &fakeTransport{cond: sync.NewCond(&sync.Mutex{})}
	f.cond.L = &f.mu
	for _, t := range tools {
		f.tools = append(f.tools, json.RawMessage(`{"name":"`+t+`","description":"`+t+`"}`))
	}
	return f
}

func (f *fakeTransport) Send(msg *jsonrpc.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var resp *jsonrpc.Message
	switch msg.Method {
	case "initialize":
		resp = &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{"serverInfo":{"name":"fake"}}`)}
	case "tools/list":
		raw, _ := json.Marshal(map[string]any{"tools": f.tools})
		resp = &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Result: raw}
	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		f.calls = append(f.calls, p.Name)
		resp = &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`)}
	default:
		resp = &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{}`)}
	}
	f.queue = append(f.queue, resp)
	f.cond.Broadcast()
	return nil
}

func (f *fakeTransport) Receive() (*jsonrpc.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.queue) == 0 {
		f.cond.Wait()
	}
	msg := f.queue[0]
	f.queue = f.queue[1:]
	return msg, nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cond.Broadcast()
	return nil
}

// newTestMux wires a two-upstream mux ("a" with read_file, "b" with
// delete_file) under a policy allowing only *__read*.
func newTestMux(t *testing.T) (*Mux, *fakeTransport, *fakeTransport) {
	t.Helper()
	writer, err := session.NewWriter(t.TempDir()+"/session.jsonl", "mux-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	policyPath := t.TempDir() + "/policy.yaml"
	if err := os.WriteFile(policyPath, []byte(`
version: 1
default: deny
rules:
  - id: allow-reads
    tool: "*__read*"
    action: allow
    reason: "reads ok"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	med := mediator.New(mediator.Options{
		SessionID: "mux-test", Evaluator: p, Confirmer: approval.Deny{},
		Writer: writer,
	})

	fa := newFakeTransport("read_file")
	fb := newFakeTransport("delete_file")
	mx := &Mux{
		med:     med,
		byName:  map[string]*Upstream{"a": {client: mcpclient.New(fa)}, "b": {client: mcpclient.New(fb)}},
		version: "test",
	}
	return mx, fa, fb
}

func serveInput(t *testing.T, mx *Mux, lines ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := mx.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestMuxNamepsacesAndRoutes(t *testing.T) {
	mx, fa, fb := newTestMux(t)

	out := serveInput(t, mx,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"a__read_file","arguments":{"path":"/x"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"b__delete_file","arguments":{}}}`,
	)

	if !strings.Contains(out, `"a__read_file"`) || !strings.Contains(out, `"b__delete_file"`) {
		t.Fatalf("tools not namespaced:\n%s", out)
	}
	if !strings.Contains(out, `"text":"done"`) {
		t.Fatalf("allowed call not routed:\n%s", out)
	}
	// delete_file is denied by policy (only *__read* allowed).
	if !strings.Contains(out, "tool_denied") || strings.Contains(out, `"id":4,"result"`) {
		t.Fatalf("policy deny missing:\n%s", out)
	}
	// Routing went to the right upstreams with un-namespaced names.
	if len(fa.calls) != 1 || fa.calls[0] != "read_file" {
		t.Fatalf("upstream a calls: %v", fa.calls)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("upstream b must not receive denied call: %v", fb.calls)
	}
}

func TestMuxUnknownServer(t *testing.T) {
	mx, _, _ := newTestMux(t)
	out := serveInput(t, mx,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"zz__nope","arguments":{}}}`,
	)
	if !strings.Contains(out, "unknown_tool") {
		t.Fatalf("want unknown_tool error:\n%s", out)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	path := t.TempDir() + "/mux.yaml"
	write := func(content string) string {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := LoadConfig(write("version: 1\nservers: [{name: a, command: [x]}]\n")); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := []string{
		"version: 2\nservers: [{name: a, command: [x]}]\n",  // wrong version
		"version: 1\nservers: []\n",                          // empty
		"version: 1\nservers: [{name: a, command: [x], url: u}]\n", // both
		"version: 1\nservers: [{name: a, command: [x]}, {name: a, command: [x]}]\n", // dup
		"version: 1\nservers: [{name: a__b, command: [x]}]\n", // bad name
	}
	for i, content := range bad {
		if _, err := LoadConfig(write(content)); err == nil {
			t.Errorf("case %d: invalid config accepted", i)
		}
	}
}

