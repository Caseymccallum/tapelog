package checkpoint

// Live Rekor round-trip — the evidence tier for the witness path
// (mirrors TAPELOG_COMPAT_LIVE: opt-in, never blocks CI):
//
//	TAPELOG_REKOR_LIVE=1 go test ./internal/checkpoint/ -run Live -v
//
// Submits a real entry to the public log and verifies the full offline
// witness chain against it. Costs one transparency-log entry per run.

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveRekorRoundTrip(t *testing.T) {
	if os.Getenv("TAPELOG_REKOR_LIVE") == "" {
		t.Skip("TAPELOG_REKOR_LIVE not set — witness path is covered by the fake; live tier is opt-in")
	}
	key, err := os.ReadFile(os.Getenv("TAPELOG_REKOR_LIVE_KEY"))
	if err != nil {
		t.Fatalf("TAPELOG_REKOR_LIVE_KEY must point at an OpenSSH private key: %v", err)
	}

	// A throwaway statement: unique per run so the log never dedups.
	stmt := Statement{Version: 1, SessionID: "live-" + time.Now().UTC().Format("20060102T150405.000Z"),
		Seq: 1, ChainHead: "live", CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")}

	ckpt := &Checkpoint{Statement: stmt}
	if err := ckpt.SignSSH(key); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := ckpt.WitnessRekor(ctx, DefaultRekorURL); err != nil {
		t.Fatalf("witness submission failed: %v", err)
	}
	t.Logf("witnessed: uuid=%s index=%d integrated=%d",
		ckpt.Witness.EntryUUID, ckpt.Witness.LogIndex, ckpt.Witness.IntegratedTime)

	// Full offline verification: entry binding + SET + inclusion + tree head.
	if wit, err := ckpt.Verify(VerifyOptions{}); err != nil || wit != "rekor" {
		t.Fatalf("offline witness verification failed: wit=%q err=%v", wit, err)
	}

	// Tamper the witness timestamp — SET must catch it.
	ckpt.Witness.IntegratedTime++
	if _, err := ckpt.Verify(VerifyOptions{}); err == nil {
		t.Error("tampered witness verified")
	}
}
