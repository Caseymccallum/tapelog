package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadVerifiedRefusesTampered pins the trust-bearing replay
// contract: replay will not quietly serve altered evidence as "the
// recorded session". The escape hatch (allowUnverified) preserves the
// tamper-testing workflow and reports the chain verdict either way.
func TestLoadVerifiedRefusesTampered(t *testing.T) {
	path := buildTapelog(t)

	// Healthy log: loads fine with the default gate.
	tape, res, err := LoadVerified(path, false)
	if err != nil || tape == nil || !res.OK() {
		t.Fatalf("healthy log must load: err=%v ok=%v", err, res != nil && res.OK())
	}

	// Tamper one recorded verdict (recomputed hashes are NOT fixed up —
	// exactly the edit an evidence-faking tool would make).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), `"verdict":"allow"`, `"verdict":"denyyy"`, 1)
	tamPath := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(tamPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	// Default gate: refuse, naming the tampering point.
	_, _, err = LoadVerified(tamPath, false)
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("tampered log must be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-unverified") {
		t.Fatalf("refusal must name the escape hatch, got %v", err)
	}

	// Escape hatch: loads, and the verdict stays visibly broken.
	tape2, res2, err := LoadVerified(tamPath, true)
	if err != nil || tape2 == nil {
		t.Fatalf("allow-unverified must load the tape: err=%v", err)
	}
	if res2 == nil || res2.OK() {
		t.Fatalf("allow-unverified must still report a broken chain, got %+v", res2)
	}
}