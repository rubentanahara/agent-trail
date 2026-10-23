package gh

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	LogTailLines    = 5000
	maxLogLineBytes = 64 << 10
	truncatedMarker = " …[line truncated]"
	inProgressText  = "still in progress"
)

var (
	actionsURL   = regexp.MustCompile(`/actions/runs/(\d+)/job/(\d+)`)
	logTimestamp = regexp.MustCompile(`^\x{FEFF}?\d{4}-\d{2}-\d{2}T[\d:.]+Z ?`)
)

type LogRef struct {
	RunID string
	JobID string
}

func ParseCheckURL(url string) (LogRef, bool) {
	match := actionsURL.FindStringSubmatch(url)
	if match == nil {
		return LogRef{}, false
	}
	return LogRef{RunID: match[1], JobID: match[2]}, true
}

func LogArgs(ref LogRef, failedOnly bool) []string {
	flag := "--log"
	if failedOnly {
		flag = "--log-failed"
	}
	return []string{"run", "view", ref.RunID, "--job=" + ref.JobID, flag}
}

func IsRunInProgress(err error) bool {
	var runErr *Error
	return errors.As(err, &runErr) && strings.Contains(runErr.Stderr, inProgressText)
}

func TailLog(r io.Reader, limit int) (lines []string, truncated bool, err error) {
	reader := bufio.NewReaderSize(r, maxLogLineBytes)
	ring := make([]string, 0, limit)
	for {
		line, last, readErr := readLogLine(reader)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, false, readErr
		}
		if line != "" || !last {
			ring = append(ring, cleanLogLine(line))
			if len(ring) > limit {
				ring = ring[1:]
				truncated = true
			}
		}
		if last {
			return ring, truncated, nil
		}
	}
}

func readLogLine(reader *bufio.Reader) (string, bool, error) {
	var line []byte
	for {
		chunk, prefix, err := reader.ReadLine()
		if err != nil {
			return string(line), true, err
		}
		if len(line) < maxLogLineBytes {
			line = append(line, chunk...)
		}
		if !prefix {
			return string(line), false, nil
		}
		if len(line) >= maxLogLineBytes {
			drainLine(reader)
			return string(line[:maxLogLineBytes]) + truncatedMarker, false, nil
		}
	}
}

func drainLine(reader *bufio.Reader) {
	for {
		_, prefix, err := reader.ReadLine()
		if err != nil || !prefix {
			return
		}
	}
}

func cleanLogLine(line string) string {
	if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
		line = parts[2]
	}
	line = logTimestamp.ReplaceAllString(line, "")
	line = ansi.Strip(line)
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)
}
