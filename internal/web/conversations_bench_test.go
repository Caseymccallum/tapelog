package web

// M14 performance: conversation-first investigation must stay cheap enough that
// opening a conversation is an interaction, not a batch job. These benchmarks
// measure the paths the panel actually uses:
//
//   - the handler that answers one conversation (grouping the whole library,
//     then slicing one conversation and one page),
//   - the two-conversation comparison,
//   - a 250-call conversation against a library that also holds a large one.
//
// Run with: go test ./internal/web -run '^$' -bench BenchmarkConversation -benchmem

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// benchLibrary writes recordings with the given per-conversation call counts
// and returns the directory.
func benchLibrary(b *testing.B, files int, conversations []int) string {
	b.Helper()
	dir := b.TempDir()
	for f := 0; f < files; f++ {
		name := fmt.Sprintf("%02d-bench.jsonl", f)
		w, err := session.NewWriter(filepath.Join(dir, name), name)
		if err != nil {
			b.Fatal(err)
		}
		add := func(kind session.EventType, v any) {
			if _, err := w.Append(kind, v); err != nil {
				b.Fatal(err)
			}
		}
		add(session.EventSessionStart, map[string]any{"harness": "fictional"})
		id := 0
		for c, calls := range conversations {
			conv := fmt.Sprintf("conv-%d-%d", f, c)
			for i := 0; i < calls; i++ {
				id++
				seq := addCall(b, w, id, conv)
				_ = seq
			}
		}
		add(session.EventSessionEnd, map[string]any{"reason": "fictional complete"})
		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

// addCall appends one allowed, answered call and returns its sequence number.
func addCall(b *testing.B, w *session.Writer, id int, conv string) uint64 {
	b.Helper()
	e, err := w.Append(session.EventToolCall, map[string]any{
		"id": id, "tool": "read_file", "args": map[string]any{"path": "/fictional"},
		"conversation_id": conv,
	})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := w.Append(session.EventPolicyDecision, map[string]any{
		"id": id, "parent_seq": e.Seq, "verdict": "allow",
	}); err != nil {
		b.Fatal(err)
	}
	if _, err := w.Append(session.EventToolResult, map[string]any{
		"id": id, "parent_seq": e.Seq, "result": map[string]any{"text": "ok"},
	}); err != nil {
		b.Fatal(err)
	}
	return e.Seq
}

func BenchmarkConversationHandler(b *testing.B) {
	// 4 recordings x 40 conversations x 10 calls = 1 600 calls, 160 groups.
	dir := benchLibrary(b, 4, tenCalls(40))
	srv := &server{dir: dir}
	mux := newMux(srv)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/behaviour/conversation?conversation=conv-0-7&limit=100", nil)
		req.Host = "127.0.0.1"
		mux.ServeHTTP(rw, req)
		if rw.Code != 200 {
			b.Fatalf("status %d: %s", rw.Code, rw.Body.String())
		}
	}
}

func tenCalls(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = 10
	}
	return out
}

// BenchmarkOverviewHandler is the baseline the conversation endpoint is judged
// against: both perform the same library analysis, so a conversation request
// must not cost meaningfully more than the overview it is sliced from.
func BenchmarkOverviewHandler(b *testing.B) {
	dir := benchLibrary(b, 4, tenCalls(40))
	srv := &server{dir: dir}
	mux := newMux(srv)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/behaviour", nil)
		req.Host = "127.0.0.1"
		mux.ServeHTTP(rw, req)
		if rw.Code != 200 {
			b.Fatalf("status %d: %s", rw.Code, rw.Body.String())
		}
	}
}

func BenchmarkCompareConversationsHandler(b *testing.B) {
	dir := benchLibrary(b, 2, tenCalls(30))
	srv := &server{dir: dir}
	mux := newMux(srv)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/behaviour/compare-conversations?a=conv-0-1&b=conv-0-2", nil)
		req.Host = "127.0.0.1"
		mux.ServeHTTP(rw, req)
		if rw.Code != 200 {
			b.Fatalf("status %d: %s", rw.Code, rw.Body.String())
		}
	}
}

// BenchmarkConversationPaging measures the expensive part on its own: one
// library-wide grouping plus slicing, without the HTTP layer.
func BenchmarkConversationPaging(b *testing.B) {
	calls := make([]behaviour.CallRecord, 0, 20000)
	for i := 0; i < 20000; i++ {
		calls = append(calls, behaviour.CallRecord{
			Seq: uint64(i + 1), Tool: "read_file", LibrarySeq: uint64(i + 1),
			ConversationID: fmt.Sprintf("conv-%d", i%200), ResultSeq: uint64(i + 2),
		})
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		groups := behaviour.GroupConversations(calls)
		page := behaviour.ConversationCalls(calls, "conv-7")
		out := make([]behaviour.TimelineEntry, 0, 100)
		for _, c := range page[:100] {
			out = append(out, behaviour.TimelineEntryFor(c))
		}
		if len(groups) != 200 || len(out) != 100 {
			b.Fatalf("unexpected shape: %d groups, %d entries", len(groups), len(out))
		}
	}
}
