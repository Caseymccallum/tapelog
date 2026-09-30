// Package packs ships batteries-included policy packs. This test is the
// dogfood harness: every pack must parse, compile its Cedar conditions,
// and enforce at least one deny — a pack that rots fails CI.
package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

func TestPacksLoadAndCompile(t *testing.T) {
	files, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("expected the pack set, found %d files", len(files))
	}
	for _, file := range files {
		file := file
		t.Run(file, func(t *testing.T) {
			p, err := policy.Load(file)
			if err != nil {
				t.Fatalf("pack must load: %v", err)
			}
			if p == nil {
				t.Fatal("pack is empty")
			}
			// Every pack must enforce at least one hard deny.
			denies := 0
			for _, r := range p.Rules {
				if r.Action == "deny" {
					denies++
				}
			}
			for _, f := range p.Flows {
				if f.Action == "deny" {
					denies++
				}
			}
			if denies == 0 {
				t.Fatal("pack must contain at least one deny rule or flow")
			}
			// Header comment sanity: packs explain themselves.
			data, _ := os.ReadFile(file)
			if !strings.Contains(string(data), "# tapelog policy pack:") {
				t.Error("pack must carry its header comment")
			}
		})
	}
}
