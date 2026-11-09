package web

// Regression for M13's cached-analysis race (F5), named so it cannot be lost.
//
// `aggregateOverview` used to stamp File/Index onto the shared, cached
// sessionAnalysis. Two concurrent behaviour requests then wrote the same slice
// (confirmed by -race at behaviour.go:800/801 versus rowFor:951), which is both
// a data race and a correctness bug: one request's file stamping leaked into
// another's answer.
//
// The fix was to stamp a copy. This test asserts the OBSERVABLE invariant — the
// cached analysis is byte-identical before and after concurrent overviews — and
// is deterministic, so it fails even on a run without the race detector. Under
// `-race` the same test fails at the write site if the copy is ever removed.

import (
	"reflect"
	"sync"
	"testing"
)

func TestCachedAnalysisIsNeverMutatedByConcurrentOverviews(t *testing.T) {
	dir := contractLibrary(t)
	srv := &server{dir: dir}
	idx, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Sessions) < 2 {
		t.Fatalf("need at least two analysed recordings, got %d", len(idx.Sessions))
	}
	// Snapshot every cached call. The fields aggregateOverview stamps are
	// populated as empty on a fresh analysis; anything else appearing here is
	// a request writing into shared state.
	type snapshot struct {
		calls [][]callRecord
	}
	before := snapshot{}
	for _, a := range idx.Sessions {
		before.calls = append(before.calls, append([]callRecord(nil), a.Calls...))
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ov := aggregateOverview(idx)
			if ov == nil || len(ov.Conversations) == 0 {
				t.Error("overview produced no conversations")
			}
		}()
	}
	wg.Wait()

	after := snapshot{}
	for _, a := range idx.Sessions {
		after.calls = append(after.calls, append([]callRecord(nil), a.Calls...))
	}
	for i := range before.calls {
		if !reflect.DeepEqual(before.calls[i], after.calls[i]) {
			t.Fatalf("recording %d: the cached analysis changed during concurrent overviews — request state is being written into the cache", i)
		}
	}
	// And the copies really do carry the stamping, or the test above would pass
	// simply because nothing was ever stamped.
	ov := aggregateOverview(idx)
	stamped := 0
	for _, g := range ov.Conversations {
		for _, e := range g.Timeline {
			if e.File != "" && e.Seq != 0 {
				stamped++
			}
		}
	}
	if stamped == 0 {
		t.Fatal("timeline entries carry no file/sequence: the overview is not stamping the copies")
	}
}
