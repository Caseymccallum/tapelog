package approval

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler serves the approval queue's local API. The trust model is
// localhost-first (docs/THREAT_MODEL.md): when token is non-empty, every
// mutating/reading endpoint requires `Authorization: Bearer <token>`.
func Handler(q *Queue, token string) http.Handler {
	mux := http.NewServeMux()

	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if token == "" {
			return true
		}
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == token {
			return true
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/pending", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !auth(w, r) {
			return
		}
		writeJSON(w, map[string]any{"pending": q.List()})
	})

	mux.HandleFunc("/decide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(w, r) {
			return
		}
		var req struct {
			ID     int    `json:"id"`
			Verdict string `json:"verdict"` // allow | allow_session | deny
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		var choice Choice
		switch req.Verdict {
		case "allow":
			choice = ChoiceAllowOnce
		case "allow_session", "session":
			choice = ChoiceAllowSession
		case "deny":
			choice = ChoiceDeny
		default:
			http.Error(w, `{"error":"verdict must be allow|allow_session|deny"}`, http.StatusBadRequest)
			return
		}
		if !q.Decide(req.ID, choice, req.Note) {
			http.Error(w, `{"error":"unknown or already resolved id"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "id": req.ID, "verdict": req.Verdict})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
