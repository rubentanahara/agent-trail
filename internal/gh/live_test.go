package gh

import (
	"context"
	"os"
	"testing"
)

func TestLiveArgumentsAreAcceptedByTheInstalledGh(t *testing.T) {
	if os.Getenv("AGENT_TRAIL_GH_LIVE") != "1" {
		t.Skip("set AGENT_TRAIL_GH_LIVE=1 to run against the installed gh")
	}
	run := Exec(".")
	ctx := context.Background()
	if _, err := LoadRepo(ctx, run); err != nil {
		t.Fatalf("LoadRepo: %v", err)
	}
	if _, err := Viewer(ctx, run); err != nil {
		t.Fatalf("Viewer: %v", err)
	}
	if _, err := List(ctx, run); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := ListReviewRequested(ctx, run); err != nil {
		t.Fatalf("ListReviewRequested: %v", err)
	}
	out, err := run(ctx, ThreadsArgs("cli", "cli", 14781))
	if err != nil {
		t.Fatalf("threads: %v", err)
	}
	if _, err := DecodeThreads(out); err != nil {
		t.Fatalf("DecodeThreads: %v", err)
	}
}
