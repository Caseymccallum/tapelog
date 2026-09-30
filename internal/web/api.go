package web

import (
	"embed"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/approval"
)

//go:embed static
var staticFS embed.FS

func newMux(s *server) http.Handler {
	mux := http.NewServeMux()

	static := func(name, contentType string) http.HandlerFunc {
		return secure(s.token, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			data, _ := staticFS.ReadFile("static/" + name)
			_, _ = w.Write(data)
		})
	}
	mux.HandleFunc("/", secure(s.token, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, _ := staticFS.ReadFile("static/index.html")
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("/app.js", static("app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/app.css", static("app.css", "text/css; charset=utf-8"))

	pending := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"GET only"}`, http.StatusMethodNotAllowed)
			return
		}
		if s.queue == nil {
			writeJSON(w, map[string]any{"pending": []any{}})
			return
		}
		writeJSON(w, map[string]any{"pending": s.queue.List()})
	}
	decide := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || s.queue == nil {
			http.Error(w, `{"error":"no approval queue"}`, http.StatusNotFound)
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
		choice, ok := parseVerdict(req.Verdict)
		if !ok {
			http.Error(w, `{"error":"verdict must be allow|allow_session|deny"}`, http.StatusBadRequest)
			return
		}
		if !s.queue.Decide(req.ID, choice, req.Note) {
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
		path := s.logPath
		if s.dir != "" {
			name := r.URL.Query().Get("file")
			if name == "" {
				writeJSON(w, map[string]any{"events": []any{}, "next": 0})
				return
			}
			resolved, ok := safeJoin(s.dir, name)
			if !ok {
				http.Error(w, `{"error":"invalid file"}`, http.StatusBadRequest)
				return
			}
			path = resolved
		}
		events, next := readLog(path, queryInt(r, "after"))
		writeJSON(w, map[string]any{"events": events, "next": next})
	}
	sessions := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"GET only"}`, http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"sessions": listSessions(s.dir)})
	}

	mux.HandleFunc("/api/pending", secure(s.token, pending))
	mux.HandleFunc("/api/decide", secure(s.token, decide))
	mux.HandleFunc("/api/log", secure(s.token, logAPI))
	mux.HandleFunc("/api/sessions", secure(s.token, sessions))
	mux.HandleFunc("/pending", secure(s.token, pending))
	mux.HandleFunc("/decide", secure(s.token, decide))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return mux
}

// parseVerdict maps API verdicts to approval choices.
func parseVerdict(v string) (approval.Choice, bool) {
	switch v {
	case "allow":
		return approval.ChoiceAllowOnce, true
	case "allow_session", "session":
		return approval.ChoiceAllowSession, true
	case "deny":
		return approval.ChoiceDeny, true
	}
	return approval.ChoiceDeny, false
}

// safeJoin resolves a client-supplied file name inside dir. The name must
// be a plain *.jsonl base name — path-traversal defense.
func safeJoin(dir, name string) (string, bool) {
	if name != strings.TrimSpace(name) || name != filepath.Base(name) || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	return filepath.Join(dir, name), true
}
