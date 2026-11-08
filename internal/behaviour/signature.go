// Package behaviour contains reusable, deterministic behaviour-analysis primitives.
package behaviour

import (
	"regexp"
	"sort"
	"strings"
)

var (
	quotedRun = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)
	urlRun    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)
	uuidRun   = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	hexRun    = regexp.MustCompile(`\b(?:0x)?[0-9a-fA-F]{12,}\b`)
	pathRun   = regexp.MustCompile(`(?:[A-Za-z]:\\[^\s"']*|(?:^|[\s(=])/(?:[^\s"'/]+/)+[^\s"',)]*)`)
	numberRun = regexp.MustCompile(`(^|[^\w])\d+(?:\.\d+)?`)
	spaceRun  = regexp.MustCompile(`\s+`)
	// trailingDetail matches a bracketed suffix such as "(line 3)", "[errno 2]"
	// or "(attempt 4)". Real servers append this context to otherwise identical
	// messages, so leaving it in would create a new group per occurrence.
	trailingDetail = regexp.MustCompile(`\s*[\(\[](?:[^\)\]]*)[\)\]]\s*$`)
)

// normaliseMessage replaces the parts of an error message that change between
// occurrences with stable placeholders, then keeps the opening words. The
// placeholder names appear in the group label, so a reader can see exactly what
// was collapsed.
func NormaliseMessage(message string) string {
	s := strings.TrimSpace(message)
	if s == "" {
		return ""
	}
	s = urlRun.ReplaceAllString(s, "<url>")
	s = uuidRun.ReplaceAllString(s, "<id>")
	s = pathRun.ReplaceAllString(s, "<path>")
	s = quotedRun.ReplaceAllString(s, "<text>")
	s = hexRun.ReplaceAllString(s, "<id>")
	s = numberRun.ReplaceAllString(s, "${1}<n>")
	s = strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
	for i := 0; i < 3; i++ {
		trimmed := strings.TrimSpace(trailingDetail.ReplaceAllString(s, ""))
		if trimmed == s || trimmed == "" {
			break
		}
		s = trimmed
	}
	words := strings.Fields(s)
	// Eight words identify a problem; a longer explanation with a variable tail
	// would otherwise become its own group every time the tail changed.
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

// signatureFragment returns a literal, value-free substring of the recorded
// message that a `result_never_contains` assertion can look for.
//
// The engine does a literal substring match, so the fragment has to appear in
// the recording exactly as written. Volatile spans — numbers, identifiers,
// paths, URLs and quoted values — are excluded, both because they would make
// the assertion unstable and because copying a recorded value into a test file
// is not something this tool should do. Among the remaining literal runs the
// longest is chosen (earliest wins a tie), which recovers a useful phrase from
// messages like `relation "users" does not exist`.
func SignatureFragment(message string) string {
	s := strings.TrimSpace(spaceRun.ReplaceAllString(message, " "))
	if s == "" {
		return ""
	}
	type span struct{ a, b int }
	var spans []span
	for _, re := range []*regexp.Regexp{urlRun, uuidRun, pathRun, quotedRun, hexRun, numberRun} {
		for _, loc := range re.FindAllStringIndex(s, -1) {
			spans = append(spans, span{loc[0], loc[1]})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	best := ""
	prev := 0
	for _, sp := range append(spans, span{len(s), len(s)}) {
		if sp.a > prev {
			cand := strings.Trim(strings.TrimSpace(s[prev:sp.a]), ":;,.<>=-([{\"'")
			// A shorter fragment would match unrelated messages, which makes the
			// generated assertion a false positive waiting to happen.
			if len(cand) >= 12 && len(cand) > len(best) {
				best = cand
			}
		}
		if sp.b > prev {
			prev = sp.b
		}
	}
	return shortText(best, 80)
}

func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "�") + "… [preview limited]"
}
