package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/approval"
)

//go:embed static/index.html static/app.js static/app.css
var staticFS embed.FS

func newMux(s *server) http.Handler {
	mux := http.NewServeMux()
	// At most two bounded snapshot scans per handler, with no unbounded queue.
	scans := make(chan struct{}, 2)
	limited := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			select {
			case scans <- struct{}{}:
				defer func() { <-scans }()
				h(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				apiError(w, 429, fmt.Errorf("workbench is reading recordings; try again shortly"))
			}
		}
	}

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
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
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
		if s.dir != "" {
			name := r.URL.Query().Get("file")
			if name == "" {
				writeJSON(w, map[string]any{"events": []any{}, "next": 0})
				return
			}
			_, ok := safeJoin(s.dir, name)
			if !ok {
				http.Error(w, `{"error":"invalid file"}`, http.StatusBadRequest)
				return
			}
		}
		rec, err := s.recording(r.URL.Query().Get("file"))
		if err != nil {
			apiError(w, 400, err)
			return
		}
		events := []json.RawMessage{}
		after, next, max := queryInt(r, "after"), queryInt(r, "after"), 0
		budget := 1 << 20
		for _, e := range rec.Events {
			if int(e.Seq) > max {
				max = int(e.Seq)
			}
			if int(e.Seq) <= after || len(events) >= 100 {
				continue
			}
			raw, _ := json.Marshal(e)
			if len(raw) > budget {
				break
			}
			budget -= len(raw)
			events = append(events, raw)
			next = int(e.Seq)
		}
		resp := map[string]any{"events": events, "next": next, "max": max, "chain_ok": rec.Info.ChainOK, "first_bad_seq": rec.Info.FirstBadSeq, "problem": rec.Info.Problem, "integrity": rec.Info.Integrity}
		writeJSON(w, resp)
	}

	mux.HandleFunc("/api/pending", secure(s.token, pending))
	mux.HandleFunc("/api/decide", secure(s.token, decide))
	mux.HandleFunc("/api/log", secure(s.token, limited(logAPI)))
	mux.HandleFunc("/api/sessions", secure(s.token, limited(s.libraryAPI)))
	mux.HandleFunc("/api/activity", secure(s.token, limited(s.activitiesAPI)))
	mux.HandleFunc("/api/events", secure(s.token, limited(s.eventsAPI)))
	mux.HandleFunc("/api/event", secure(s.token, limited(s.eventAPI)))
	mux.HandleFunc("/api/compare", secure(s.token, limited(s.compareAPI)))
	mux.HandleFunc("/api/behaviour", secure(s.token, limited(s.behaviourAPI)))
	mux.HandleFunc("/api/behaviour/repeats", secure(s.token, limited(s.repeatsAPI)))
	mux.HandleFunc("/api/behaviour/signature", secure(s.token, limited(s.signatureAPI)))
	mux.HandleFunc("/api/behaviour/delta", secure(s.token, limited(s.deltaAPI)))

	mux.HandleFunc("/api/test", secure(s.token, limited(s.testAPI)))
	mux.HandleFunc("/api/policy/policies", secure(s.token, limited(s.policiesAPI)))
	mux.HandleFunc("/api/policy/inspect", secure(s.token, limited(s.inspectAPI)))
	mux.HandleFunc("/api/policy/from-event", secure(s.token, s.fromEventAPI))
	mux.HandleFunc("/api/policy/preview", secure(s.token, s.previewAPI))
	mux.HandleFunc("/api/policy/simulate", secure(s.token, limited(s.simulateAPI)))
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
	if name == "" || strings.ContainsAny(name, "/\\:\x00") || name != strings.TrimSpace(name) || name != filepath.Base(name) || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	return filepath.Join(dir, name), true
}
