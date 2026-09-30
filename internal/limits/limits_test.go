package limits

import (
	"testing"
	"time"
)

func TestBudgetAndPerTool(t *testing.T) {
	tr, err := NewTracker(Limits{MaxCalls: 3, MaxCallsPerTool: map[string]int{"shell_*": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if v := tr.Check("read_file"); v != nil {
		t.Fatalf("call 1 must pass: %+v", v)
	}
	if v := tr.Check("shell_exec"); v != nil {
		t.Fatalf("shell 1 must pass: %+v", v)
	}
	if v := tr.Check("shell_exec"); v == nil || v.RuleID != "limits.max_calls_per_tool" {
		t.Fatalf("shell 2 must hit per-tool limit, got %+v", v)
	}
	if v := tr.Check("read_file"); v != nil {
		t.Fatalf("call 3 must pass: %+v", v)
	}
	if v := tr.Check("read_file"); v == nil || v.RuleID != "limits.max_calls" {
		t.Fatalf("call 4 must hit budget, got %+v", v)
	}
}

func TestRateWindow(t *testing.T) {
	tr, err := NewTracker(Limits{MaxPerMinute: 2})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tr.now = func() time.Time { return now }
	if v := tr.Check("a"); v != nil {
		t.Fatal(v)
	}
	if v := tr.Check("a"); v != nil {
		t.Fatal(v)
	}
	if v := tr.Check("a"); v == nil || v.RuleID != "limits.max_per_minute" {
		t.Fatalf("3rd call inside the minute must be limited, got %+v", v)
	}
	now = now.Add(61 * time.Second)
	if v := tr.Check("a"); v != nil {
		t.Fatalf("window must slide open: %+v", v)
	}
}

func TestResponseCap(t *testing.T) {
	tr, err := NewTracker(Limits{MaxResponseBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if v := tr.CheckResponse(10); v != nil {
		t.Fatalf("at the cap is allowed: %+v", v)
	}
	if v := tr.CheckResponse(11); v == nil || v.RuleID != "limits.max_response_bytes" {
		t.Fatalf("over the cap must trip, got %+v", v)
	}
}

func TestValidation(t *testing.T) {
	if _, err := NewTracker(Limits{MaxCalls: -1}); err == nil {
		t.Error("negative must fail")
	}
	if _, err := NewTracker(Limits{MaxCallsPerTool: map[string]int{"shell_*": -1}}); err == nil {
		t.Error("negative per-tool must fail")
	}
	if !(Limits{MaxCalls: 1}).Enabled() || (Limits{}).Enabled() {
		t.Error("Enabled() wrong")
	}
}
