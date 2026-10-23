package gh

import (
	"errors"
	"testing"
)

func splitFixture(t *testing.T) map[string]FileDiff {
	t.Helper()
	byPath := map[string]FileDiff{}
	for _, file := range SplitDiff(string(fixture(t, "diff.patch"))) {
		byPath[file.Path] = file
	}
	return byPath
}

func TestSplitDiffCountsSkipFileHeaders(t *testing.T) {
	files := splitFixture(t)

	guide := files["docs/guide.md"]
	if guide.Added != 2 || guide.Removed != 1 {
		t.Fatalf("guide counts = +%d -%d, want +2 -1", guide.Added, guide.Removed)
	}
	removed := files["removed.txt"]
	if removed.Added != 0 || removed.Removed != 2 {
		t.Fatalf("removed counts = +%d -%d, want +0 -2", removed.Added, removed.Removed)
	}
}

func TestSplitDiffHandlesRenamesAndBinaryFiles(t *testing.T) {
	files := splitFixture(t)

	renamed, ok := files["new/name.go"]
	if !ok || !renamed.Renamed || renamed.OldPath != "old/name.go" {
		t.Fatalf("renamed = %+v", renamed)
	}
	if renamed.Added != 1 || renamed.Removed != 1 {
		t.Fatalf("renamed counts = +%d -%d", renamed.Added, renamed.Removed)
	}
	logo := files["logo.png"]
	if !logo.Binary || logo.Added != 0 || logo.Removed != 0 {
		t.Fatalf("logo = %+v", logo)
	}
}

func TestSplitDiffNumbersRightSideLines(t *testing.T) {
	guide := splitFixture(t)["docs/guide.md"]

	var numbered []int
	for _, line := range guide.Lines {
		if line.Kind == LineAdded || line.Kind == LineContext {
			numbered = append(numbered, line.NewLine)
		}
	}
	want := []int{1, 2, 3, 4, 5}
	if len(numbered) != len(want) {
		t.Fatalf("numbered = %v, want %v", numbered, want)
	}
	for i := range want {
		if numbered[i] != want[i] {
			t.Fatalf("numbered = %v, want %v", numbered, want)
		}
	}
}

func TestSplitDiffKeepsNoNewlineMarkerOutOfTheCounts(t *testing.T) {
	patch := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n"
	files := SplitDiff(patch)

	if len(files) != 1 || files[0].Added != 1 || files[0].Removed != 1 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Lines[len(files[0].Lines)-1].Kind != LineMeta {
		t.Fatalf("marker kind = %v", files[0].Lines[len(files[0].Lines)-1].Kind)
	}
}

func TestIsDiffTooLarge(t *testing.T) {
	large := &Error{Stderr: "could not find pull request diff: HTTP 406: Sorry, the diff exceeded the maximum number of lines", Err: errors.New("exit status 1")}
	if !IsDiffTooLarge(large) {
		t.Fatal("HTTP 406 should be too large")
	}
	if IsDiffTooLarge(errors.New("other")) {
		t.Fatal("foreign error is not too large")
	}
}
