// Package web serves tapelog's local review dashboard: the approval
// queue UI plus a live session-log viewer, on the same localhost socket
// as --approval-listen. Security posture (docs/THREAT_MODEL.md):
//
//   - Untrusted data (tool names, args, results) is rendered with
//     textContent only — never HTML — plus a strict same-origin CSP.
//     Tool output is attacker-controlled; stored XSS here would be a
//     boundary bypass.
//   - Without a token, the Host header must be loopback (DNS-rebinding
//     defense) and cross-origin POSTs are refused (CSRF defense).
//   - With --approval-token, every /api/* call needs the bearer token.
//   - Responses carry Cache-Control: no-store (logs are sensitive).
package web

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/approval"
)

//go:embed static
var staticFS embed.FS

// Handler builds the dashboard + API mux.
//
//	q       — the approval queue to review/decide
//	logPath — the session JSONL to display (may be empty: viewer hides)
//	token   — bearer token; "" = loopback-only, no auth
func Handler(q *approval.Queue, logPath, token string) http.Handler {
	mux := http.NewServeMux()

	secure := func(h http.HandlerFunc) http.HandlerFunc {
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

	mux.HandleFunc("/", secure(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, _ := staticFS.ReadFile("static/index.html")
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("/app.js", secure(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		data, _ := staticFS.ReadFile("static/app.js")
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("/app.css", secure(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		data, _ := staticFS.ReadFile("static/app.css")
		_, _ = w.Write(data)
	}))

	// JSON API: /api/* plus legacy aliases so `tapelog queue` keeps working.
	pending := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"GET only"}`, http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"pending": q.List()})
	}
	decide := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID      int    `json:"id"`
			Verdict string `json:"verdict"`
			Note    string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		var choice approval.Choice
		switch req.Verdict {
		case "allow":
			choice = approval.ChoiceAllowOnce
		case "allow_session", "session":
			choice = approval.ChoiceAllowSession
		case "deny":
			choice = approval.ChoiceDeny
		default:
			http.Error(w, `{"error":"verdict must be allow|allow_session|deny"}`, http.StatusBadRequest)
			return
		}
		if !q.Decide(req.ID, choice, req.Note) {
			http.Error(w, `{"error":"unknown or already resolved id"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "id": req.ID, "verdict": req.Verdict})
	}
	logAPI := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"GET only"}`, http.StatusMethodNotAllowed)
			return
		}
		after := queryInt(r, "after")
		events, next := readLog(logPath, after)
		writeJSON(w, map[string]any{"events": events, "next": next})
	}

	mux.HandleFunc("/api/pending", secure(pending))
	mux.HandleFunc("/api/decide", secure(decide))
	mux.HandleFunc("/api/log", secure(logAPI))
	mux.HandleFunc("/pending", secure(pending))
	mux.HandleFunc("/decide", secure(decide))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return mux
}
