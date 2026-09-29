package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tapelog-dev/tapelog/internal/jsonrpc"
)

func TestHTTPSingleJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer srv.Close()

	h := NewHTTP(srv.URL, nil)
	if err := h.Send(&jsonrpc.Message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}); err != nil {
		t.Fatal(err)
	}
	msg, err := h.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if !msg.IsResponse() || string(msg.ID) != "1" {
		t.Fatalf("bad response: %+v", msg)
	}
}

func TestHTTPSSEresponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[]}}\n\n"))
	}))
	defer srv.Close()

	h := NewHTTP(srv.URL, nil)
	if err := h.Send(&jsonrpc.Message{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/call"}); err != nil {
		t.Fatal(err)
	}
	msg, err := h.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.ID) != "2" {
		t.Fatalf("bad SSE response: %+v", msg)
	}
}

func TestHTTPSendsHeaders(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{}}`))
	}))
	defer srv.Close()

	h := NewHTTP(srv.URL, map[string]string{"Authorization": "Bearer tok123"})
	if err := h.Send(&jsonrpc.Message{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "ping"}); err != nil {
		t.Fatal(err)
	}
	_, _ = h.Receive()
	if gotAuth != "Bearer tok123" {
		t.Fatalf("headers not forwarded: %q", gotAuth)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := NewHTTP(srv.URL, nil)
	err := h.Send(&jsonrpc.Message{JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "ping"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want HTTP 500 error, got %v", err)
	}
}
