package compat

import (
	"encoding/json"
	"testing"
)

// send is one request through the scripted server, parsed back into the
// result object. Deliberately raw JSON rather than a typed struct: the defect
// these tests exist for was invisible to a typed client that has no opinion
// about fields it does not know about, and a struct here would repeat the
// mistake by simply omitting them again.
func send(t *testing.T, s *Server, method string) map[string]any {
	t.Helper()
	in := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `"}`)
	out := s.Handle(in)
	if len(out) == 0 {
		t.Fatalf("%s: server produced no response", method)
	}
	var msg struct {
		Result map[string]any  `json:"result"`
		Error  *struct{ Code int `json:"code"` } `json:"error"`
	}
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatalf("%s: response is not JSON: %v\n%s", method, err, out)
	}
	if msg.Error != nil {
		return map[string]any{"__errorCode": float64(msg.Error.Code)}
	}
	return msg.Result
}

// assertCacheDirectives checks the two mandatory 2026-07-28 caching hints, and
// checks their *shape* — a hint of the wrong type is as unusable to a strict
// client as a missing one.
func assertCacheDirectives(t *testing.T, what string, result map[string]any) {
	t.Helper()

	raw, ok := result["ttlMs"]
	if !ok {
		t.Fatalf("%s: missing ttlMs (2026-07-28 requires caching hints on cacheable results)", what)
	}
	ttl, ok := raw.(float64)
	if !ok {
		t.Fatalf("%s: ttlMs must be a JSON number, got %T", what, raw)
	}
	if ttl < 0 {
		t.Fatalf("%s: ttlMs must be >= 0 (schema minimum), got %v", what, ttl)
	}
	if ttl != fixtureTTLMs {
		t.Fatalf("%s: ttlMs = %v, want the conservative fixture default %d", what, ttl, fixtureTTLMs)
	}

	raw, ok = result["cacheScope"]
	if !ok {
		t.Fatalf("%s: missing cacheScope (2026-07-28 requires caching hints on cacheable results)", what)
	}
	scope, ok := raw.(string)
	if !ok {
		t.Fatalf("%s: cacheScope must be a string, got %T", what, raw)
	}
	if scope != "public" && scope != "private" {
		t.Fatalf("%s: cacheScope = %q, want one of public|private", what, scope)
	}
	if scope != fixtureCacheScope {
		t.Fatalf("%s: cacheScope = %q, want the conservative fixture default %q", what, scope, fixtureCacheScope)
	}
}

func assertNoCacheDirectives(t *testing.T, what string, result map[string]any) {
	t.Helper()
	for _, field := range []string{"ttlMs", "cacheScope"} {
		if _, present := result[field]; present {
			t.Fatalf("%s: legacy era must not emit %s — that is a different protocol's shape", what, field)
		}
	}
}

// TestModernCacheableResultsCarryMandatoryHints is the regression for the H1.1
// defect: the 2026-07-28 fixture answered tools/list and server/discover without
// ttlMs/cacheScope, which a strict client rejects outright with
// "Field required" — the fixture looked healthy to tapelog's own lenient Go
// client and to every scripted probe, and failed only in front of a real SDK.
func TestModernCacheableResultsCarryMandatoryHints(t *testing.T) {
	s := NewServer(Era2026)

	list := send(t, s, "tools/list")
	assertCacheDirectives(t, "2026-07-28 tools/list", list)
	if list["resultType"] != "complete" {
		t.Fatalf("2026-07-28 tools/list: resultType = %v, want \"complete\"", list["resultType"])
	}
	tools, ok := list["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("2026-07-28 tools/list: tools must be a non-empty array, got %T", list["tools"])
	}

	discover := send(t, s, "server/discover")
	assertCacheDirectives(t, "2026-07-28 server/discover", discover)
	if discover["resultType"] != "complete" {
		t.Fatalf("2026-07-28 server/discover: resultType = %v, want \"complete\"", discover["resultType"])
	}
	versions, ok := discover["supportedVersions"].([]any)
	if !ok || len(versions) == 0 {
		t.Fatalf("2026-07-28 server/discover: supportedVersions missing or empty")
	}
	if versions[0] != string(Era2026) {
		t.Fatalf("2026-07-28 server/discover: supportedVersions[0] = %v, want %q", versions[0], Era2026)
	}
}

// TestLegacyErasKeepTheirOwnShape is the other half: the fix must not
// modernise the older revisions. A 2024 client that suddenly received caching
// hints would be parsing a protocol it never agreed to, and the compat lab's
// job is to reproduce each era faithfully.
func TestLegacyErasKeepTheirOwnShape(t *testing.T) {
	for _, era := range []Era{Era2025, Era2024} {
		s := NewServer(era)

		list := send(t, s, "tools/list")
		assertNoCacheDirectives(t, string(era)+" tools/list", list)
		if _, ok := list["tools"].([]any); !ok {
			t.Fatalf("%s tools/list: tools missing", era)
		}

		discover := send(t, s, "server/discover")
		// Legacy servers predate discovery; a method-not-found is what drives a
		// dual-era client's fallback, so it must stay exactly that.
		if code, ok := discover["__errorCode"].(float64); !ok || code != -32601 {
			t.Fatalf("%s server/discover: want -32601 method not found, got %v", era, discover)
		}
	}
}

// TestCacheDirectivesAreGatedOnEra pins the helper itself, so a future caller
// cannot reach past the era check by accident.
func TestCacheDirectivesAreGatedOnEra(t *testing.T) {
	for _, tc := range []struct {
		era  Era
		want bool
	}{
		{Era2026, true},
		{Era2025, false},
		{Era2024, false},
	} {
		got := withCacheDirectives(tc.era, map[string]any{})
		_, present := got["ttlMs"]
		if present != tc.want {
			t.Fatalf("withCacheDirectives(%q): present=%v, want %v", tc.era, present, tc.want)
		}
	}
}
