package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const (
	defaultEditor    = "vi"
	draftFilePattern = "agent-trail-*.md"
	editorWaitNotice = "waiting for editor…"
)

type composeKind int

const (
	composeComment composeKind = iota
	composeReply
	composeEditComment
	composeEditBody
	composeReview
	composeLine
	composeSubmit
)

type composePurpose struct {
	kind      composeKind
	number    int
	threadID  string
	commentID string
	inline    bool
	event     gh.ReviewEvent
	submit    gh.SubmitEvent
	path      string
	line      int
	headSHA   string
}

type composedMsg struct {
	purpose composePurpose
	path    string
	err     error
}

func (composedMsg) isPRMessage() {}

func editorCommand(path string) *exec.Cmd {
	fields := strings.Fields(firstSet(os.Getenv("VISUAL"), os.Getenv("EDITOR"), defaultEditor))
	return exec.Command(fields[0], append(fields[1:], path)...)
}

func firstSet(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (m *model) compose(purpose composePurpose, seed string) tea.Cmd {
	file, err := os.CreateTemp("", draftFilePattern)
	if err != nil {
		m.notice = "could not create a draft file: " + err.Error()
		return nil
	}
	_, writeErr := file.WriteString(seed)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(file.Name())
		m.notice = "could not write the draft file"
		return nil
	}
	m.notice = editorWaitNotice
	path := file.Name()
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		return composedMsg{purpose: purpose, path: path, err: err}
	})
}

func readDraft(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.TrimSpace(strings.ReplaceAll(text, "\r", "\n")), nil
}

func discardDraft(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func (m *model) absorbComposed(msg composedMsg) tea.Cmd {
	body, err := readDraft(msg.path)
	switch {
	case msg.err != nil:
		discardDraft(msg.path)
		m.notice = "the editor failed: " + sanitize(msg.err.Error())
		return nil
	case err != nil:
		m.notice = "could not read the draft: " + err.Error()
		return nil
	case body == "" && !msg.purpose.allowsEmpty():
		discardDraft(msg.path)
		m.notice = "empty message, cancelled"
		return nil
	}
	return m.sendComposed(msg.purpose, body, msg.path)
}

func (p composePurpose) allowsEmpty() bool {
	return p.kind == composeReview && p.event == gh.ReviewApprove
}
