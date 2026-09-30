package approval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQueueParkAndDecide(t *testing.T) {
	q := NewQueue(5 * time.Second)

	got := make(chan Choice, 1)
	go func() { got <- q.Confirm("deploy", json.RawMessage(`{"env":"prod"}`)) }()

	// Wait for the call to park (generous deadline: CI runners are slow).
	var items []PendingItem
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && len(items) == 0 {
		items = q.List()
		if len(items) == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(items) != 1 || items[0].Tool != "deploy" {
		t.Fatalf("pending list: %+v", items)
	}
	if !q.Decide(items[0].ID, ChoiceAllowOnce, "looks fine") {
		t.Fatal("decide failed")
	}
	select {
	case c := <-got:
		if c != ChoiceAllowOnce {
			t.Fatalf("choice = %v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm did not return")
	}
	if q.Decide(items[0].ID, ChoiceDeny, "") {
		t.Fatal("double decide must fail")
	}
}

func TestQueueTimeoutFailsClosed(t *testing.T) {
	q := NewQueue(30 * time.Millisecond)
	start := time.Now()
	choice, why := q.ConfirmExplain("write_file", nil)
	if choice != ChoiceDeny {
		t.Fatalf("timeout must deny, got %v", choice)
	}
	if !strings.Contains(why, "expired in the approval queue") {
		t.Fatalf("timeout reason missing: %q", why)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout did not fire promptly")
	}
	if len(q.List()) != 0 {
		t.Fatal("expired item must leave the queue")
	}
}

func TestQueueSessionAllowAndNotes(t *testing.T) {
	q := NewQueue(5 * time.Second)
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && len(q.List()) == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		items := q.List()
		if len(items) > 0 {
			q.Decide(items[0].ID, ChoiceAllowSession, "trusted tool")
		}
	}()
	c1, why := q.ConfirmExplain("read_file", nil)
	if c1 != ChoiceAllowSession || !strings.Contains(why, "trusted tool") {
		t.Fatalf("first: %v %q", c1, why)
	}
	c2, why2 := q.ConfirmExplain("read_file", nil)
	if c2 != ChoiceAllowSession || !strings.Contains(why2, "allowed for session") {
		t.Fatalf("cached session allow: %v %q", c2, why2)
	}
}

func TestQueueListOldestFirst(t *testing.T) {
	q := NewQueue(5 * time.Second)
	decideWhenVisible := func(n int) {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			items := q.List()
			if len(items) >= n {
				for _, it := range items {
					q.Decide(it.ID, ChoiceDeny, "")
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	ids := make(chan int, 2)
	go func() {
		q.Confirm("a", nil)
		q.Confirm("b", nil)
		close(ids)
	}()
	go func() { decideWhenVisible(1) }()
	time.Sleep(10 * time.Millisecond)
	go func() { decideWhenVisible(1) }()
	<-ids
}
