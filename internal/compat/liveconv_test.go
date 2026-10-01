package compat

import (
	"strings"
	"testing"
)

func rawSchema(required []string) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	for i, r := range required {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + r + `":{"type":"string"}`)
	}
	b.WriteString(`},"required":[`)
	for i, r := range required {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + r + `"`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func TestPlanLiveSafeOnly(t *testing.T) {
	catalog := []ToolInfo{
		{Name: "a__read_file", Schema: rawSchema([]string{"path"})},
		{Name: "a__write_file", Schema: rawSchema([]string{"path", "content"})},
		{Name: "a__delete_file", Schema: rawSchema([]string{"path"})},
		{Name: "a__send_http", Schema: rawSchema([]string{"url", "body"})},
	}
	passA, plan := planLive(catalog)
	joined := strings.Join(passA, "\n")

	// Mutating tools with no policy deny must never execute for real.
	if strings.Contains(joined, "a__write_file") {
		t.Errorf("write_file must not be called against a real server:\n%s", joined)
	}
	if strings.Contains(joined, "a__send_http") {
		t.Errorf("send_http (uncontaminated) must not be called:\n%s", joined)
	}
	// The delete probe IS sent (policy denies it pre-forward) and expected.
	if !strings.Contains(joined, "a__delete_file") {
		t.Errorf("rule-deny probe for delete_file missing:\n%s", joined)
	}
	if len(plan.WantDenies) != 1 || plan.WantDenies[0] != "no-delete" {
		t.Errorf("WantDenies = %v, want [no-delete]", plan.WantDenies)
	}
	// Redaction probe present, canary planted under a sensitive key.
	if !plan.Redaction || !strings.Contains(joined, LiveRedactionCanary) {
		t.Errorf("redaction probe missing or canary absent:\n%s", joined)
	}
	if !strings.Contains(joined, "api_key") {
		t.Errorf("canary must be under a sensitive field name:\n%s", joined)
	}
	// No taint source tool in this catalog -> no source, no sink.
	if plan.TaintSource != "" || plan.TaintSink != "" {
		t.Errorf("taint flow must not run without read_secrets: %+v", plan)
	}
}

func TestPlanLiveTaintFlow(t *testing.T) {
	catalog := []ToolInfo{
		{Name: "a__read_secrets", Schema: rawSchema(nil)},
		{Name: "a__send_http", Schema: rawSchema([]string{"url", "body"})},
	}
	passA, plan := planLive(catalog)
	if plan.TaintSource != "a__read_secrets" {
		t.Fatalf("taint source not planned: %+v", plan)
	}
	if strings.Contains(strings.Join(passA, "\n"), "a__send_http") {
		t.Errorf("sink must not be in Pass A:\n%s", strings.Join(passA, "\n"))
	}

	// Pass B: the value is copied from the recorded result verbatim.
	src := `{"content":[{"type":"text","text":"sk-CANARY-7f3a9b"}]}`
	lines, sink := planTaintSink(catalog, src)
	if sink != "a__send_http" {
		t.Fatalf("sink = %q, want a__send_http", sink)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "sk-CANARY-7f3a9b") {
		t.Errorf("sink args must carry the contaminating value:\n%s", strings.Join(lines, "\n"))
	}

	// Redaction-marker-only results produce no sink (nothing to taint).
	if lines, sink := planTaintSink(catalog, `{"text":"[REDACTED]"}`); sink != "" || lines != nil {
		t.Errorf("marker-only result must not produce a sink: %v %q", lines, sink)
	}
}

func TestBenignArgsRequiredOnly(t *testing.T) {
	ti := ToolInfo{Name: "a__query", Schema: rawSchema([]string{"query", "url", "path"})}
	args := benignArgs(ti)
	if args["query"] != "SELECT 1" {
		t.Errorf("query arg = %v, want SELECT 1", args["query"])
	}
	if args["url"] != "https://example.com" {
		t.Errorf("url arg = %v", args["url"])
	}
	if args["path"] != "tapelog-compat-probe.txt" {
		t.Errorf("path arg = %v", args["path"])
	}
}

func TestIsReadish(t *testing.T) {
	for name, want := range map[string]bool{
		"a__read_file": true, "a__get_file_info": true, "a__list_directory": true,
		"a__query": true, "a__search": true, "a__fetch": true,
		"a__write_file": false, "a__delete_file": false, "a__send_http": false,
		"a__push_files": false, "a__create_repository": false, "a__run_query": false,
	} {
		if got := isReadish(name); got != want {
			t.Errorf("isReadish(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestGlobMatchPolicyPatterns(t *testing.T) {
	if !globMatch("*__delete_file", "a__delete_file") {
		t.Error("policy pattern must match namespaced delete_file")
	}
	if globMatch("*__delete_file", "a__delete_files") {
		t.Error("pattern must not over-match")
	}
	if !globMatch("*__read_secrets", "a__read_secrets") || !globMatch("*__send_*", "a__send_http") {
		t.Error("flow patterns must match")
	}
}

func TestCatalogFromHarnessOutput(t *testing.T) {
	out := `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","tools":[{"name":"a__read_file","inputSchema":{"type":"object"}}]}}`
	cat := CatalogFromHarnessOutput(out)
	if len(cat) != 1 || cat[0].Name != "a__read_file" {
		t.Fatalf("catalog parse failed: %+v", cat)
	}
}
