//go:build !linux

package main

import "github.com/Caseymccallum/tapelog/internal/sandbox"

// execPlatform is only reachable in lenient mode off Linux (the command
// runs without restrictions; the caller warned about it).
func execPlatform(command []string) error { return sandbox.ErrUnsupported }
