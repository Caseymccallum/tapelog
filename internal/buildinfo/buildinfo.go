// Package buildinfo is the single source of truth for the tapelog build
// version. Everything that identifies itself to the outside world — the
// CLI (`tapelog version`), the MCP clientInfo we send upstreams, the mux
// serverInfo, and the replay server — reads it from here, so internal and
// documented version numbers cannot drift apart. goreleaser stamps it via
// -X at release time (see .goreleaser.yaml).
package buildinfo

// Version is the tapelog build version (stamped at release; dev default).
var Version = "0.3.0-dev"