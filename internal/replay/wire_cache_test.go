package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestServeEmitsModernCacheDirectives is the replay-side regression for the same
// H1.1 defect the scripted fixture had: `server/discover` and `tools/list`
// answers carried `resultType: "complete"` but neither caching hint, and MCP
// 2026-07-28 makes both mandatory on a cacheable result.
//
// The failure mode is worth naming, because it is the reason this test asserts
// raw JSON rather than a typed struct: a strict client rejects the whole
// ListToolsResult ("cacheScope: Field required"), so replay connected, looked
// healthy, and offered **zero usable tools** — a recording that replays into
// nothing. A lenient client (tapelog's own) sees nothing wrong at all.
func TestServeEmitsModernCacheDirectives(t *testing.T) {
	c, _ := Load(buildTapelog(t))
	s := &Server{Player: NewPlayer(c, MatchExact), Version: "test"}

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n")

	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}

	results := map[int]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg struct {
			ID     int            `json:"id"`
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("replay produced a non-JSON line %q: %v", line, err)
		}
		if msg.Result != nil {
			results[msg.ID] = msg.Result
		}
	}

	for id, what := range map[int]string{1: "server/discover", 2: "tools/list"} {
		res, ok := results[id]
		if !ok {
			t.Fatalf("%s: replay produced no result", what)
		}
		if res["resultType"] != "complete" {
			t.Fatalf("%s: resultType = %v, want \"complete\"", what, res["resultType"])
		}

		ttl, ok := res["ttlMs"].(float64)
		if !ok {
			t.Fatalf("%s: missing or non-numeric ttlMs (2026-07-28 requires caching hints)", what)
		}
		if ttl != replayTTLMs {
			t.Fatalf("%s: ttlMs = %v, want %d (immediately stale, never a positive TTL)", what, ttl, replayTTLMs)
		}

		scope, ok := res["cacheScope"].(string)
		if !ok {
			t.Fatalf("%s: missing or non-string cacheScope (2026-07-28 requires caching hints)", what)
		}
		if scope != replayCacheScope || (scope != "public" && scope != "private") {
			t.Fatalf("%s: cacheScope = %q, want %q", what, scope, replayCacheScope)
		}
	}
}
