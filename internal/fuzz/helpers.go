package fuzz

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// insertZeroWidth splits the value with a zero-width space (evades
// substring/`like` matching rules that matched the original).
func insertZeroWidth(s string) string {
	if len(s) < 4 {
		return s
	}
	mid := len(s) / 2
	return s[:mid] + "\u200b" + s[mid:]
}

// base64 encodes a value (classic rule-evasion transform).
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// urlEncode percent-encodes a value.
func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// clip gives a short label for finding descriptions.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 24 {
		s = s[:24] + "..."
	}
	return s
}
