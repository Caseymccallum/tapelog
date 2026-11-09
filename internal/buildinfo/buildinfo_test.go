package buildinfo

import (
	"regexp"
	"strings"
	"testing"
)

// TestVersionIdentifiesTheCandidate guards the release-candidate identity. A source
// build used to report "0.4.0" — the same string as the published release — so the
// banner could not answer "is this the release or a development build?".
func TestVersionIdentifiesTheCandidate(t *testing.T) {
	if Version == "0.4.0" {
		t.Fatal("the development line must not report the published v0.4.0 identity")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+-`).MatchString(Version) {
		t.Fatalf("a prerelease identity must carry a hyphenated suffix, got %q", Version)
	}
	if strings.Contains(Version, " ") || strings.Contains(Version, "\t") {
		t.Fatalf("version must be one token suitable for archive names, got %q", Version)
	}
}

// TestCommitIsEmptyByDefault keeps the "no provenance" signal honest: a plain
// `go build` must be distinguishable from a stamped candidate build, which prints
// the commit beside the version.
func TestCommitIsEmptyByDefault(t *testing.T) {
	if Commit != "" {
		t.Fatalf("a plain build must not claim a commit; ldflags stamp it (%q)", Commit)
	}
}
