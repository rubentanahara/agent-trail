package trail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	hunkMarker         = "@@"
	gitDiffMarker      = "diff --git "
	gitDiffNewPathMark = " b/"
	addedSign          = "+"
	removedSign        = "-"
	claudePatchMarker  = `"structuredPatch"`
	claudeBashMarker   = `"bashEditDiff"`
	claudeCreateType   = "create"
	codexAddType       = "add"
	codexDeleteType    = "delete"
)

type FileChange struct {
	Path    string
	Added   int
	Removed int
	Counted bool
}

type FilePatch struct {
	Path  string
	Lines []string
}

func LoadPatch(event Event) ([]FilePatch, error) {
	line, err := readLineAt(event.Source.Path, event.Source.Offset)
	if err != nil {
		return nil, err
	}
	events, err := newParsers[event.CLI](event.Source.Path).parse(line, &Session{})
	if err != nil || event.Source.Index >= len(events) {
		return nil, err
	}
	return events[event.Source.Index].patches, nil
}

func countChanges(patches []FilePatch) []FileChange {
	var changes []FileChange
	for _, patch := range patches {
		change := FileChange{Path: patch.Path, Counted: len(patch.Lines) > 0}
		for _, line := range patch.Lines {
			change.Added += btoi(strings.HasPrefix(line, addedSign))
			change.Removed += btoi(strings.HasPrefix(line, removedSign))
		}
		changes = append(changes, change)
	}
	return changes
}

func btoi(yes bool) int {
	if yes {
		return 1
	}
	return 0
}

type claudeHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

type claudeToolResult struct {
	FilePath        string       `json:"filePath"`
	Type            string       `json:"type"`
	Content         string       `json:"content"`
	StructuredPatch []claudeHunk `json:"structuredPatch"`
	BashEditDiff    *struct {
		Files []struct {
			FilePath string       `json:"filePath"`
			Hunks    []claudeHunk `json:"hunks"`
		} `json:"files"`
	} `json:"bashEditDiff"`
}

func attachClaudePatches(events []Event, toolUseResult json.RawMessage) {
	if !bytes.Contains(toolUseResult, []byte(claudePatchMarker)) && !bytes.Contains(toolUseResult, []byte(claudeBashMarker)) {
		return
	}
	var result claudeToolResult
	if json.Unmarshal(toolUseResult, &result) != nil {
		return
	}
	for i := range events {
		if events[i].Kind == KindResult {
			events[i].patches = result.patches()
			return
		}
	}
}

func (r claudeToolResult) patches() []FilePatch {
	var patches []FilePatch
	if r.FilePath != "" {
		lines := claudeHunkLines(r.StructuredPatch)
		if len(lines) == 0 && r.Type == claudeCreateType {
			lines = wholeFileLines(r.Content, addedSign)
		}
		patches = append(patches, FilePatch{Path: r.FilePath, Lines: lines})
	}
	if r.BashEditDiff != nil {
		for _, file := range r.BashEditDiff.Files {
			patches = append(patches, FilePatch{Path: file.FilePath, Lines: claudeHunkLines(file.Hunks)})
		}
	}
	return patches
}

func claudeHunkLines(hunks []claudeHunk) []string {
	var lines []string
	for _, hunk := range hunks {
		lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines))
		lines = append(lines, hunk.Lines...)
	}
	return lines
}

func wholeFileLines(content, sign string) []string {
	body := strings.TrimSuffix(content, "\n")
	if body == "" {
		return nil
	}
	rows := strings.Split(body, "\n")
	header := fmt.Sprintf("@@ -0,0 +1,%d @@", len(rows))
	if sign == removedSign {
		header = fmt.Sprintf("@@ -1,%d +0,0 @@", len(rows))
	}
	lines := []string{header}
	for _, row := range rows {
		lines = append(lines, sign+row)
	}
	return lines
}

func codexPatches(changes map[string]codexFileChange) []FilePatch {
	patches := make([]FilePatch, 0, len(changes))
	for _, path := range slices.Sorted(maps.Keys(changes)) {
		patches = append(patches, FilePatch{Path: path, Lines: changes[path].lines()})
	}
	return patches
}

func (c codexFileChange) lines() []string {
	switch c.Type {
	case codexAddType:
		return wholeFileLines(c.Content, addedSign)
	case codexDeleteType:
		return wholeFileLines(c.Content, removedSign)
	}
	return hunkLines(c.UnifiedDiff)
}

func copilotPatches(data copilotData) []FilePatch {
	if !data.Success {
		return nil
	}
	var result struct {
		DetailedContent string `json:"detailedContent"`
	}
	_ = json.Unmarshal(data.Result, &result)
	diffs := parseGitDiff(result.DetailedContent)
	patches := make([]FilePatch, 0, len(data.FileEdits))
	for _, edit := range data.FileEdits {
		patches = append(patches, diffOf(diffs, edit.Path))
	}
	return patches
}

func diffOf(diffs []FilePatch, path string) FilePatch {
	for _, diff := range diffs {
		if diff.Path == path {
			return diff
		}
	}
	return FilePatch{Path: path}
}

func parseGitDiff(text string) []FilePatch {
	var patches []FilePatch
	sections := strings.Split("\n"+text, "\n"+gitDiffMarker)
	for _, section := range sections[1:] {
		header, body, _ := strings.Cut(section, "\n")
		patches = append(patches, FilePatch{Path: gitDiffPath(header), Lines: hunkLines(body)})
	}
	return patches
}

func gitDiffPath(header string) string {
	at := strings.LastIndex(header, gitDiffNewPathMark)
	if at < 0 {
		return header
	}
	return "/" + header[at+len(gitDiffNewPathMark):]
}

func hunkLines(diff string) []string {
	_, rest, found := strings.Cut("\n"+diff, "\n"+hunkMarker)
	if !found {
		return nil
	}
	return strings.Split(strings.TrimRight(hunkMarker+rest, "\n"), "\n")
}
