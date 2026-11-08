package web

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestM7WorkflowMatchesWebCountsAndDeterminism(t *testing.T) {
	dir := t.TempDir()
	calls := []bc{{tool: "db__query", args: map[string]any{"password": "NEVER_SHOW_ARGUMENT"}, verdict: "allow", result: map[string]any{"message": "NEVER_SHOW_SECRET in result"}, isError: true}, {tool: "db__query", result: "ok", verdict: "allow"}, {tool: "db__delete", verdict: "deny"}}
	writeBehaviourFile(t, dir, "a.jsonl", calls)
	writeBehaviourFile(t, dir, "b.jsonl", calls)
	rw, body := requestJSON(t, DirHandler(dir, ""), "GET", "/api/behaviour", nil)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var expected struct {
		Counts behaviourCounts `json:"counts"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &expected); err != nil {
		t.Fatal(err)
	}
	_ = body
	var first []byte
	for i := 0; i < 15; i++ {
		got, err := BehaviourWorkflow(dir, nil, "summary")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "complete" {
			t.Fatalf("status %s", got.Status)
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "NEVER_SHOW") || strings.Contains(string(raw), secretArg) {
			t.Fatal("payload leaked", string(raw))
		}
		if i == 0 {
			first = raw
		} else if string(first) != string(raw) {
			t.Fatal("nondeterministic JSON")
		}
		result := got.Result.(map[string]any)
		counts := result["counts"].(map[string]any)
		if int(counts["calls"].(float64)) != expected.Counts.Calls || int(counts["failures"].(float64)) != expected.Counts.Failures {
			t.Fatalf("CLI/web counts differ: %+v %+v", counts, expected.Counts)
		}
	}
}
func TestM7IncompleteInvalidInputsAndConfinement(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFileOpts(t, dir, "incomplete.jsonl", []bc{{tool: "x"}}, false)
	r, err := BehaviourWorkflow(dir, []string{"incomplete.jsonl"}, "summary")
	if err != nil || r.Status != "partial" {
		t.Fatal(r, err)
	}
	if _, err = BehaviourWorkflow(dir, []string{"../outside.jsonl"}, "summary"); err == nil {
		t.Fatal("accepted traversal")
	}
	if _, err = BehaviourWorkflow(dir, []string{"incomplete.jsonl", "incomplete.jsonl"}, "summary"); err == nil {
		t.Fatal("accepted duplicate")
	}
	if _, err = BehaviourWorkflowCompare(dir, "incomplete.jsonl", "../outside.jsonl"); err == nil {
		t.Fatal("accepted compare traversal")
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err = os.WriteFile(outside, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "link.jsonl")); err == nil {
		r, err = BehaviourWorkflow(dir, []string{"link.jsonl"}, "summary")
		if err != nil || r.Status != "partial" {
			t.Fatal("symlink accepted", r, err)
		}
	} else {
		t.Logf("symlink test unavailable: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = BehaviourWorkflow(dir, []string{"broken.jsonl"}, "summary")
	if err != nil || r.Status != "partial" {
		t.Fatal("invalid chain not partial", r, err)
	}
}
func TestM7ConcurrentFreshDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFile(t, dir, "a.jsonl", []bc{{tool: "read", result: "ok"}})
	var wg sync.WaitGroup
	results := make([][]byte, 6)
	errs := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, e := BehaviourWorkflow(dir, nil, "summary")
			errs[i] = e
			if e == nil {
				results[i], errs[i] = json.Marshal(r)
			}
		}(i)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("concurrent scan %d differed: %v", i, errs[i])
		}
	}
}
func TestM7CompareRatesAndToolUsage(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFile(t, dir, "a.jsonl", []bc{{tool: "read", result: "ok"}})
	writeBehaviourFile(t, dir, "b.jsonl", []bc{{tool: "write", verdict: "deny"}})
	r, err := BehaviourWorkflowCompare(dir, "a.jsonl", "b.jsonl")
	if err != nil || r.Status != "complete" {
		t.Fatal(r, err)
	}
	m := r.Result.(map[string]any)
	raw, _ := json.Marshal(m["tool_usage"])
	var tools map[string]struct {
		Reference int `json:"reference"`
		Candidate int `json:"candidate"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	if tools["read"].Reference != 1 || tools["write"].Candidate != 1 {
		t.Fatal(tools)
	}

	rates := m["rates"].([]any)
	found := false
	for _, item := range rates {
		v := item.(map[string]any)
		if strings.HasPrefix(v["metric"].(string), "Tool-error") {
			found = true
			if v["current"] != nil || v["current_n"].(float64) != 0 {
				t.Fatal(v)
			}
		}
	}
	if !found {
		t.Fatal("missing rate")
	}
}
func TestM7CacheRejectsSameSizeSameMtimeMutation(t *testing.T) {
	dir := t.TempDir()
	path := writeBehaviourFile(t, dir, "recording.jsonl", []bc{{tool: "read", result: "ok"}})
	srv := &server{dir: dir}
	first, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil || !first.Sessions[0].Info.ChainOK {
		t.Fatal(first, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), `"result":"ok"`, `"result":"no"`, 1)
	if changed == string(raw) || len(changed) != len(raw) {
		t.Fatal("fixture mutation failed")
	}
	if err = os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cached != 0 || second.Sessions[0].Info.ChainOK {
		t.Fatal("stale verified cache accepted changed bytes")
	}
}
func TestM7LargeLibraryReportsLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 125; i++ {
		writeBehaviourFile(t, dir, fmt.Sprintf("%03d.jsonl", i), []bc{{tool: "read", result: "ok"}})
	}
	r, err := BehaviourWorkflow(dir, nil, "summary")
	if err != nil || r.Status != "partial" {
		t.Fatal(r, err)
	}
	lib := r.Result.(map[string]any)["library"].(map[string]any)
	if lib["analysed"].(float64) != 120 || lib["truncated"] != true {
		t.Fatal(lib)
	}
}
func BenchmarkM7Directory100Files1000Calls(b *testing.B) {
	dir := b.TempDir()
	calls := make([]bc, 10)
	for i := range calls {
		calls[i] = bc{tool: "read", args: map[string]any{"item": i}, result: "fictional"}
	}
	for i := 0; i < 100; i++ {
		writeBehaviourFile(b, dir, fmt.Sprintf("%03d.jsonl", i), calls)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := BehaviourWorkflow(dir, nil, "summary")
		if err != nil || r.Status != "complete" {
			b.Fatal(r, err)
		}
	}
}
func TestM7CompareMatchesWebCore(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFile(t, dir, "a.jsonl", []bc{{tool: "read", result: "ok"}})
	writeBehaviourFile(t, dir, "b.jsonl", []bc{{tool: "delete", verdict: "deny"}})
	rw, _ := requestJSON(t, DirHandler(dir, ""), "GET", "/api/behaviour/delta?a=a.jsonl&b=b.jsonl", nil)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var webResult any
	if err := json.Unmarshal(rw.Body.Bytes(), &webResult); err != nil {
		t.Fatal(err)
	}
	safe, err := workflowSafe(webResult)
	if err != nil {
		t.Fatal(err)
	}
	result, err := BehaviourWorkflowCompare(dir, "a.jsonl", "b.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	want := safe.(map[string]any)
	got := result.Result.(map[string]any)
	for _, field := range []string{"reference", "current", "new_denials", "resolved_denials", "new_failures", "resolved_failures", "new_repeats", "resolved_repeats", "rates"} {
		if !reflect.DeepEqual(want[field], got[field]) {
			t.Fatalf("%s differs: web=%v CLI=%v", field, want[field], got[field])
		}
	}
	raw1, _ := json.Marshal(result)
	for i := 0; i < 10; i++ {
		again, e := BehaviourWorkflowCompare(dir, "a.jsonl", "b.jsonl")
		if e != nil {
			t.Fatal(e)
		}
		raw2, _ := json.Marshal(again)
		if string(raw1) != string(raw2) {
			t.Fatal("comparison JSON not deterministic")
		}
	}
}
