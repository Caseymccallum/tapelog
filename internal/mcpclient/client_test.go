package mcpclient

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
)

// scriptedServer is an in-memory transport whose answers are scripted per
// method; it records every method it receives for assertions.
type scriptedServer struct {
	mu      sync.Mutex
	cond    *sync.Cond
	queue   []*jsonrpc.Message
	methods []string
	answers map[string]func(msg *jsonrpc.Message) *jsonrpc.Message
}

func newScriptedServer() *scriptedServer {
	s := &scriptedServer{cond: sync.NewCond(&sync.Mutex{}), answers: map[string]func(*jsonrpc.Message) *jsonrpc.Message{}}
	s.cond.L = &s.mu
	return s
}

func (s *scriptedServer) Send(ctx context.Context, msg *jsonrpc.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods = append(s.methods, msg.Method)
	if fn, ok := s.answers[msg.Method]; ok {
		if resp := fn(msg); resp != nil {
			s.queue = append(s.queue, resp)
			s.cond.Broadcast()
		}
		return nil
	}
	// Unknown method: JSON-RPC method-not-found (legacy servers do this).
	s.queue = append(s.queue, &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID,
		Error: &jsonrpc.ErrorObj{Code: -32601, Message: "method not found"}})
	s.cond.Broadcast()
	return nil
}

func (s *scriptedServer) Receive() (*jsonrpc.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 {
		s.cond.Wait()
	}
	msg := s.queue[0]
	s.queue = s.queue[1:]
	return msg, nil
}

func (s *scriptedServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cond.Broadcast()
	return nil
}

func (s *scriptedServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.methods...)
}

func resultOf(id json.RawMessage, v string) *jsonrpc.Message {
	return &jsonrpc.Message{JSONRPC: "2.0", ID: id, Result: json.RawMessage(v)}
}

func TestConnectModernServerNeedsNoHandshake(t *testing.T) {
	s := newScriptedServer()
	s.answers["server/discover"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		return resultOf(msg.ID, `{"resultType":"complete","supportedVersions":["2026-07-28"],"capabilities":{"tools":{}},`+
			`"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"modern-srv","version":"2.0"}}}`)
	}
	s.answers["initialize"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		t.Error("modern server must not receive initialize")
		return nil
	}
	c := New(s)
	defer c.Close()

	name, err := c.Connect(context.Background(), "tapelog-test")
	if err != nil {
		t.Fatal(err)
	}
	if name != "modern-srv" {
		t.Fatalf("server name = %q, want modern-srv", name)
	}
	for _, m := range s.seen() {
		if m == "initialize" || m == "notifications/initialized" {
			t.Fatalf("modern era must be handshake-less; saw %q", m)
		}
	}
}

func TestConnectLegacyServerFallsBackToInitialize(t *testing.T) {
	s := newScriptedServer()
	s.answers["initialize"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		return resultOf(msg.ID, `{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"legacy-srv","version":"1.0"}}`)
	}
	c := New(s)
	defer c.Close()

	name, err := c.Connect(context.Background(), "tapelog-test")
	if err != nil {
		t.Fatal(err)
	}
	if name != "legacy-srv" {
		t.Fatalf("server name = %q, want legacy-srv", name)
	}
	seen := strings.Join(s.seen(), ",")
	if !strings.Contains(seen, "server/discover") {
		t.Fatalf("probe missing: %s", seen)
	}
	if !strings.Contains(seen, "initialize") {
		t.Fatalf("legacy handshake missing: %s", seen)
	}
	if !strings.Contains(seen, "notifications/initialized") {
		t.Fatalf("initialized notification missing: %s", seen)
	}
	if got := c.currentProtocol(); got != "2025-11-25" {
		t.Fatalf("negotiated protocol = %q, want 2025-11-25", got)
	}
}

func TestConnectRejectsNoMutualVersion(t *testing.T) {
	s := newScriptedServer()
	s.answers["server/discover"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		return resultOf(msg.ID, `{"resultType":"complete","supportedVersions":["2099-01-01"],"capabilities":{}}`)
	}
	c := New(s)
	defer c.Close()

	if _, err := c.Connect(context.Background(), "tapelog-test"); err == nil {
		t.Fatal("want error when no mutually supported version exists")
	}
}
func TestRequestCarriesMetaSelfDescription(t *testing.T) {
	s := newScriptedServer()
	var rawParams string
	s.answers["tools/list"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		rawParams = string(msg.Params)
		return resultOf(msg.ID, `{"tools":[]}`)
	}
	c := New(s)
	defer c.Close()
	c.setName("my-harness")

	if _, err := c.Request(context.Background(), "tools/list", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal([]byte(rawParams), &p); err != nil {
		t.Fatalf("params not an object with _meta: %v (%s)", err, rawParams)
	}
	for _, key := range []string{
		"io.modelcontextprotocol/protocolVersion",
		"io.modelcontextprotocol/clientCapabilities",
		"io.modelcontextprotocol/clientInfo",
	} {
		if _, ok := p.Meta[key]; !ok {
			t.Fatalf("_meta missing %s in %s", key, rawParams)
		}
	}
	if !strings.Contains(string(p.Meta["io.modelcontextprotocol/clientInfo"]), "my-harness") {
		t.Fatalf("clientInfo should carry the client name: %s", rawParams)
	}
}

func TestUnsupportedProtocolVersionRetriesWithSupported(t *testing.T) {
	s := newScriptedServer()
	calls := 0
	s.answers["tools/list"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		calls++
		if calls == 1 {
			return &jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: &jsonrpc.ErrorObj{
				Code: CodeUnsupportedProtocolVersion, Message: "Unsupported protocol version",
				Data: json.RawMessage(`{"supported":["2025-11-25"],"requested":"2026-07-28"}`),
			}}
		}
		return resultOf(msg.ID, `{"tools":[]}`)
	}
	c := New(s)
	defer c.Close()

	resp, err := c.Request(context.Background(), "tools/list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("retry should succeed, got %v", resp.Error)
	}
	if calls != 2 {
		t.Fatalf("want one retry, saw %d calls", calls)
	}
	if got := c.currentProtocol(); got != "2025-11-25" {
		t.Fatalf("protocol after negotiation = %q, want 2025-11-25", got)
	}
}

func TestCallToolParamsForwardsExtraFields(t *testing.T) {
	s := newScriptedServer()
	var rawParams string
	s.answers["tools/call"] = func(msg *jsonrpc.Message) *jsonrpc.Message {
		rawParams = string(msg.Params)
		return resultOf(msg.ID, `{"resultType":"complete","content":[]}`)
	}
	c := New(s)
	defer c.Close()

	params := json.RawMessage(`{"name":"t","arguments":{"a":1},"inputResponses":[{"x":"y"}]}`)
	if _, _, err := c.CallToolParams(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawParams, `"inputResponses"`) {
		t.Fatalf("MRTR inputResponses dropped from forwarded params: %s", rawParams)
	}
	if !strings.Contains(rawParams, `"_meta"`) {
		t.Fatalf("_meta missing from forwarded params: %s", rawParams)
	}
}
