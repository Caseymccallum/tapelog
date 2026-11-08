package web

import (
	"net/http"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/approval"
)

// server is the dashboard state: a live approval queue and/or a session
// log (single file or a directory of logs).
type server struct {
	queue   *approval.Queue // may be nil: standalone log viewer
	logPath string          // single-session mode
	dir     string          // multi-session mode (directory of *.jsonl)
	token   string

	// policyPath is an optional, read-only reference to the policy file this
	// workspace enforces. The Policy Studio reads it to explain the rules
	// that are actually in use; nothing in the workbench writes to it.
	policyPath string
}

// Handler serves the live review dashboard (approval queue + session log).
func Handler(q *approval.Queue, logPath, token string) http.Handler {
	return HandlerWithPolicy(q, logPath, "", token)
}

// HandlerWithPolicy is Handler plus a read-only policy reference for the
// Policy Studio.
func HandlerWithPolicy(q *approval.Queue, logPath, policyPath, token string) http.Handler {
	return newMux(&server{queue: q, logPath: logPath, policyPath: policyPath, token: token})
}

// DirHandler serves a read-only dashboard over a directory of session
// logs (`tapelog web --dir`): session list + per-session log viewer.
func DirHandler(dir, token string) http.Handler {
	return DirHandlerWithPolicy(dir, "", token)
}

// DirHandlerWithPolicy is DirHandler plus a read-only policy reference for
// the Policy Studio.
func DirHandlerWithPolicy(dir, policyPath, token string) http.Handler {
	return newMux(&server{dir: dir, policyPath: policyPath, token: token})
}

// secure is the shared middleware: security headers, loopback Host
// pinning (no token), bearer auth (with token), same-origin POSTs.
func secure(token string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		if token == "" && !isLoopbackHost(r.Host) {
			http.Error(w, `{"error":"loopback hosts only"}`, http.StatusForbidden)
			return
		}
		if token != "" {
			if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, `{"error":"cross-origin refused"}`, http.StatusForbidden)
			return
		}
		h(w, r)
	}
}
