package gh

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const (
	diffHeaderPrefix = "diff --git "
	noNewlineMarker  = `\ No newline at end of file`
	binaryMarker     = "Binary files "
	renameFromPrefix = "rename from "
	renameToPrefix   = "rename to "
	tooLargeMarker   = "HTTP 406"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
	LineHunk
	LineMeta
)

type DiffLine struct {
	Kind    LineKind
	Text    string
	NewLine int
}

type FileDiff struct {
	Path    string
	OldPath string
	Renamed bool
	Binary  bool
	Added   int
	Removed int
	Lines   []DiffLine
}

func DiffArgs(number int) []string {
	return []string{"pr", "diff", strconv.Itoa(number)}
}

func IsDiffTooLarge(err error) bool {
	var runErr *Error
	return errors.As(err, &runErr) && strings.Contains(runErr.Stderr, tooLargeMarker)
}

func SplitDiff(patch string) []FileDiff {
	var files []FileDiff
	var current *FileDiff
	newLine := 0
	inHunk := false
	for _, line := range strings.Split(strings.TrimRight(patch, "\n"), "\n") {
		if header, ok := strings.CutPrefix(line, diffHeaderPrefix); ok {
			files = append(files, FileDiff{Path: pathOfHeader(header)})
			current, inHunk = &files[len(files)-1], false
			current.OldPath = oldPathOfHeader(header)
			continue
		}
		if current == nil {
			continue
		}
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			newLine, _ = strconv.Atoi(match[1])
			inHunk = true
			current.Lines = append(current.Lines, DiffLine{Kind: LineHunk, Text: line})
			continue
		}
		if !inHunk {
			current.absorbMeta(line)
			continue
		}
		newLine = current.absorbBody(line, newLine)
	}
	return files
}

func (f *FileDiff) absorbMeta(line string) {
	switch {
	case strings.HasPrefix(line, renameToPrefix):
		f.Renamed, f.Path = true, strings.TrimPrefix(line, renameToPrefix)
	case strings.HasPrefix(line, renameFromPrefix):
		f.OldPath = strings.TrimPrefix(line, renameFromPrefix)
	case strings.HasPrefix(line, binaryMarker):
		f.Binary = true
		f.Lines = append(f.Lines, DiffLine{Kind: LineMeta, Text: line})
	}
}

func (f *FileDiff) absorbBody(line string, newLine int) int {
	switch {
	case line == noNewlineMarker:
		f.Lines = append(f.Lines, DiffLine{Kind: LineMeta, Text: line})
	case strings.HasPrefix(line, "+"):
		f.Added++
		f.Lines = append(f.Lines, DiffLine{Kind: LineAdded, Text: line, NewLine: newLine})
		return newLine + 1
	case strings.HasPrefix(line, "-"):
		f.Removed++
		f.Lines = append(f.Lines, DiffLine{Kind: LineRemoved, Text: line})
	default:
		f.Lines = append(f.Lines, DiffLine{Kind: LineContext, Text: line, NewLine: newLine})
		return newLine + 1
	}
	return newLine
}

func pathOfHeader(header string) string {
	_, after, found := strings.Cut(header, " b/")
	if !found {
		return header
	}
	return after
}

func oldPathOfHeader(header string) string {
	before, _, _ := strings.Cut(header, " b/")
	return strings.TrimPrefix(before, "a/")
}
