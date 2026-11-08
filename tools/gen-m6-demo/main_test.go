package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratorRefusesExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "01-baseline-release.jsonl")
	sentinel := []byte("do not overwrite")
	if err := os.WriteFile(path, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir); err == nil {
		t.Fatal("expected refusal")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(sentinel) {
		t.Fatalf("existing recording changed: %q %v", got, err)
	}
}
