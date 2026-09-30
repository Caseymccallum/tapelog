package approval

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func parkOne(t *testing.T, q *Queue) int {
	t.Helper()
	done := make(chan Choice, 1)
	go func() { done <- q.Confirm("write_file", json.RawMessage(`{"path":"/x"}`)) }()
	for i := 0; i < 500; i++ {
		if items := q.List(); len(items) == 1 {
			return items[0].ID
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("call did not park")
	return 0
}

func TestHTTPPendingAndDecide(t *testing.T) {
	q := NewQueue(5 * time.Second)
	srv := httptest.NewServer(Handler(q, ""))
	defer srv.Close()

	id := parkOne(t, q)

	resp, err := srv.Client().Get(srv.URL + "/pending")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Pending []PendingItem `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(body.Pending) != 1 || body.Pending[0].Tool != "write_file" {
		t.Fatalf("pending payload: %+v", body.Pending)
	}

	post := httptest.NewRequest("POST", srv.URL+"/decide",
		strings.NewReader(`{"id":`+itoa(id)+`,"verdict":"allow","note":"reviewed"}`))
	rw := httptest.NewRecorder()
	Handler(q, "").ServeHTTP(rw, post)
	if rw.Code != 200 {
		t.Fatalf("decide status %d: %s", rw.Code, rw.Body.String())
	}

	// Double-decide is 404.
	post2 := httptest.NewRequest("POST", srv.URL+"/decide",
		strings.NewReader(`{"id":`+itoa(id)+`,"verdict":"deny"}`))
	rw2 := httptest.NewRecorder()
	Handler(q, "").ServeHTTP(rw2, post2)
	if rw2.Code != 404 {
		t.Fatalf("double decide status %d", rw2.Code)
	}

	// Bad verdict is 400.
	post3 := httptest.NewRequest("POST", srv.URL+"/decide", strings.NewReader(`{"id":1,"verdict":"maybe"}`))
	rw3 := httptest.NewRecorder()
	Handler(q, "").ServeHTTP(rw3, post3)
	if rw3.Code != 400 {
		t.Fatalf("bad verdict status %d", rw3.Code)
	}
}

func TestHTTPAuth(t *testing.T) {
	q := NewQueue(5 * time.Second)
	h := Handler(q, "sekret")

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "/pending", nil))
	if rw.Code != 401 {
		t.Fatalf("no-token status %d", rw.Code)
	}

	rw2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/pending", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rw2, req)
	if rw2.Code != 200 {
		t.Fatalf("with-token status %d", rw2.Code)
	}

	// healthz stays open for probes.
	rw3 := httptest.NewRecorder()
	h.ServeHTTP(rw3, httptest.NewRequest("GET", "/healthz", nil))
	if rw3.Code != 200 {
		t.Fatalf("healthz status %d", rw3.Code)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
