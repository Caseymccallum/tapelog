// Command fakecmd is the hermetic compatibility lab's stdio MCP server: a
// tiny standalone binary wrapping compat.Server, spawned as a real
// subprocess by the matrix's stdio cells (and by the canonical demo).
//
// Usage: fakecmd -era=2026-07-28|2025-11-25|2024-11-05
//
// Speaks newline-delimited JSON-RPC on stdin/stdout; exits on stdin EOF.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/Caseymccallum/tapelog/internal/compat"
)

func main() {
	era := flag.String("era", string(compat.Era2026), "protocol era to speak: 2026-07-28 | 2025-11-25 | 2024-11-05")
	flag.Parse()

	valid := false
	for _, e := range compat.Eras {
		if string(e) == *era {
			valid = true
		}
	}
	if !valid {
		fmt.Fprintf(os.Stderr, "fakecmd: unknown era %q (want one of %v)\n", *era, compat.Eras)
		os.Exit(2)
	}

	s := compat.NewServer(compat.Era(*era))
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if resp := s.Handle(append([]byte(nil), line...)); len(resp) > 0 {
			if _, err := os.Stdout.Write(resp); err != nil {
				os.Exit(1)
			}
		}
	}
}
