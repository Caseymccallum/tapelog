// Package buildinfo is the single source of truth for the tapelog build
// version. Everything that identifies itself to the outside world — the
// CLI (`tapelog version`), the MCP clientInfo we send upstreams, the mux
// serverInfo, and the replay server — reads it from here, so internal and
// documented version numbers cannot drift apart. goreleaser stamps it via
// -X at release time (see .goreleaser.yaml).
package buildinfo

// Version is the tapelog build version.
//
// A source build of an unreleased line must NOT report the same string as a
// published release: "0.4.0" was indistinguishable from the tagged binary and made
// every "which binary is this?" question unanswerable from the banner alone. The
// default here is the public-beta identity; goreleaser overrides it for tagged
// releases (see .goreleaser.yaml).
var Version = "0.5.0-beta.1"

// Commit is the git commit the binary was built from, stamped at build time:
//
//	go build -ldflags "-X github.com/Caseymccallum/tapelog/internal/buildinfo.Commit=$(git rev-parse --short HEAD)"
//
// Empty for a plain `go build`, which is itself the signal that the binary carries
// no build provenance.
var Commit = ""