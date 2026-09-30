package web

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// isLoopbackHost reports whether the Host header targets a loopback name
// (defense against DNS rebinding: an attacker's domain must not be able
// to point at the listener and read the dashboard).
func isLoopbackHost(host string) bool {
	h := host
	if strings.HasPrefix(h, "[") {
		if i := strings.Index(h, "]"); i > 0 {
			h = h[1:i]
		}
	} else if i := strings.LastIndex(h, ":"); i > 0 {
		h = h[:i]
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// sameOrigin refuses cross-site POSTs (CSRF). A browser fetch from
// another origin always carries an Origin header; same-origin or
// non-browser clients either omit it or match the Host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}

// queryInt parses a non-negative int query parameter.
func queryInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	if n < 0 {
		return 0
	}
	return n
}

// readLog returns session events with seq > after (capped) plus the
// highest seq served and the highest seq in the file at all — the last
// one lets the client detect truncation/replacement (a shrunken max
// means the log on disk is not the one it has been rendering). The
// JSONL is written by the session writer; it is opened read-only per
// request (redaction already happened at write).
func readLog(path string, after int) (events []json.RawMessage, next, max int) {
	next = after
	if path == "" {
		return nil, next, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, next, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var env struct {
			Seq int `json:"seq"`
		}
		line := sc.Bytes()
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Seq > max {
			max = env.Seq
		}
		if env.Seq > after {
			events = append(events, json.RawMessage(append([]byte(nil), line...)))
			if env.Seq > next {
				next = env.Seq
			}
		}
	}
	const maxBuf = 500
	if len(events) > maxBuf {
		events = events[len(events)-maxBuf:]
	}
	return events, next, max
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
