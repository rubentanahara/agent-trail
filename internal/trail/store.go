package trail

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type parser interface {
	parse(raw []byte, session *Session) ([]Event, error)
}

type source struct {
	cli        CLI
	candidates func() []string
	belongs    func(path, repo string) (belongs, decided bool)
	newParser  func(path string) parser
	sessionID  func(path string) string
}

func sources(home, repo string) []source {
	return []source{
		{Claude, func() []string { return claudeCandidates(home, repo) }, claudeBelongs, newClaudeParser, claudeSessionID},
		{Codex, func() []string { return codexCandidates(home) }, codexBelongs, newCodexParser, codexSessionID},
		{Copilot, func() []string { return copilotCandidates(home) }, copilotBelongs, newCopilotParser, copilotSessionID},
	}
}

type transcript struct {
	source     *source
	parser     parser
	sessionKey string
	offset     int64
	hideBefore int64
	parkedSize int64
	parked     bool
}

type Batch struct {
	Events   []Event
	Sessions []Session
}

type Trail struct {
	home        string
	repo        string
	cutoff      time.Time
	sources     []source
	transcripts map[string]*transcript
	rejected    map[string]bool
	undecided   map[string]int64
	sessions    map[string]*Session
	changed     map[string]bool
	threadNames map[string]string
	indexSize   int64
}

func New(home, repo string) *Trail {
	return &Trail{
		home:        home,
		repo:        repo,
		sources:     sources(home, repo),
		transcripts: map[string]*transcript{},
		rejected:    map[string]bool{},
		undecided:   map[string]int64{},
		sessions:    map[string]*Session{},
		changed:     map[string]bool{},
		indexSize:   -1,
	}
}

func (t *Trail) Start(cutoff time.Time) Batch {
	t.cutoff = cutoff
	var events []Event
	for _, path := range t.discover() {
		file := t.transcripts[path]
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Before(cutoff) {
			file.parked, file.parkedSize = true, info.Size()
			continue
		}
		events = append(events, t.read(path, file)...)
	}
	return t.finish(events)
}

func (t *Trail) Poll() Batch {
	var events []Event
	for _, path := range t.discover() {
		events = append(events, t.read(path, t.transcripts[path])...)
	}
	for path, file := range t.transcripts {
		size := fileSize(path)
		switch {
		case file.parked && size != file.parkedSize && size >= 0:
			file.parked, file.hideBefore = false, file.parkedSize
			events = append(events, t.read(path, file)...)
		case file.parked:
		case size >= 0 && size < file.offset:
			file.offset, file.hideBefore, file.parser = 0, 0, file.source.newParser(path)
			events = append(events, t.read(path, file)...)
		case size > file.offset:
			events = append(events, t.read(path, file)...)
		}
	}
	return t.finish(events)
}

func (t *Trail) finish(events []Event) Batch {
	t.refreshThreadNames()
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	batch := Batch{Events: events}
	for key := range t.changed {
		batch.Sessions = append(batch.Sessions, t.sessions[key].clone())
	}
	t.changed = map[string]bool{}
	return batch
}

func (t *Trail) discover() []string {
	var accepted []string
	for i := range t.sources {
		for _, path := range t.sources[i].candidates() {
			if t.decide(path, &t.sources[i]) {
				accepted = append(accepted, path)
			}
		}
	}
	return accepted
}

func (t *Trail) decide(path string, src *source) bool {
	if t.transcripts[path] != nil || t.rejected[path] {
		return false
	}
	size := fileSize(path)
	if previous, waiting := t.undecided[path]; waiting && previous == size {
		return false
	}
	belongs, decided := src.belongs(path, t.repo)
	if !decided {
		t.undecided[path] = size
		return false
	}
	delete(t.undecided, path)
	if !belongs {
		t.rejected[path] = true
		return false
	}
	t.transcripts[path] = &transcript{source: src, parser: src.newParser(path), sessionKey: string(src.cli) + ":" + src.sessionID(path)}
	return true
}

func (t *Trail) session(file *transcript, path string) *Session {
	key := file.sessionKey
	session := t.sessions[key]
	if session == nil {
		session = &Session{Key: key, CLI: file.source.cli, ID: file.source.sessionID(path)}
		t.sessions[key] = session
	}
	t.changed[key] = true
	return session
}

func (t *Trail) read(path string, file *transcript) []Event {
	handle, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer handle.Close()
	if _, err := handle.Seek(file.offset, io.SeekStart); err != nil {
		return nil
	}
	session := t.session(file, path)
	var events []Event
	reader := bufio.NewReader(handle)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		lineOffset := file.offset
		file.offset += int64(len(line))
		for _, event := range t.parseLine(line, file, session, Source{Path: path, Offset: lineOffset}) {
			if lineOffset >= file.hideBefore && !event.Time.Before(t.cutoff) {
				session.Events++
				events = append(events, event)
			}
		}
	}
	if file.source.cli == Copilot {
		applyWorkspace(session, copilotWorkspace(path))
	}
	return events
}

func (t *Trail) parseLine(line []byte, file *transcript, session *Session, at Source) []Event {
	events, err := file.parser.parse(line, session)
	if err != nil {
		events = unparsed(line, file.source.cli, err)
	}
	for i := range events {
		events[i].Session = session.Key
		events[i].Source = Source{Path: at.Path, Offset: at.Offset, Index: i}
		events[i].Summary = Clip(events[i].Text)
		events[i].Text = ""
		session.touch(events[i].Time)
		if events[i].Kind == KindUser && events[i].Agent == "" {
			session.setFallbackName(events[i].Summary)
		}
		if events[i].Usage != nil {
			session.addUsage(*events[i].Usage)
		}
	}
	return events
}

func unparsed(line []byte, cli CLI, cause error) []Event {
	var envelope struct {
		Timestamp string `json:"timestamp"`
	}
	var syntaxError *json.SyntaxError
	if errors.As(cause, &syntaxError) || json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	at, ok := parseTime(envelope.Timestamp)
	if !ok {
		return nil
	}
	sample := bytes.TrimSpace(line)
	if len(sample) > SummaryLimit*2 {
		sample = sample[:SummaryLimit*2]
	}
	return []Event{{Time: at, CLI: cli, Kind: KindUnparsed, Text: fmt.Sprintf("%v %s", cause, sample)}}
}

func applyWorkspace(session *Session, fields map[string]string) {
	session.setName(fields["name"], nameRankCustomTitle)
	if branch := fields["branch"]; branch != "" {
		session.Branch = branch
	}
}

func (t *Trail) refreshThreadNames() {
	size := fileSize(filepath.Join(t.home, ".codex", "session_index.jsonl"))
	if size != t.indexSize {
		t.indexSize = size
		t.threadNames = codexThreadNames(t.home)
		for key, session := range t.sessions {
			if session.CLI == Codex {
				t.changed[key] = true
			}
		}
	}
	for key := range t.changed {
		session := t.sessions[key]
		if session.CLI == Codex {
			session.setName(t.threadNames[session.ID], nameRankCustomTitle)
		}
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

type Detail struct {
	Text string
	Raw  string
}

func LoadDetail(event Event) (Detail, error) {
	line, err := readLineAt(event.Source.Path, event.Source.Offset)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{Raw: prettyJSON(line)}
	if event.Usage != nil {
		detail.Text = usageDetail(*event.Usage)
		return detail, nil
	}
	fresh := parserFor(event.CLI, event.Source.Path)
	events, err := fresh.parse(line, &Session{})
	if err != nil || event.Source.Index >= len(events) {
		detail.Text = event.Summary
		return detail, nil
	}
	detail.Text = events[event.Source.Index].Text
	return detail, nil
}

func parserFor(cli CLI, path string) parser {
	switch cli {
	case Claude:
		return newClaudeParser(path)
	case Codex:
		return newCodexParser(path)
	default:
		return newCopilotParser(path)
	}
}

func readLineAt(path string, offset int64) ([]byte, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	if _, err := handle.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return bufio.NewReader(handle).ReadBytes('\n')
}

func prettyJSON(line []byte) string {
	var out bytes.Buffer
	if json.Indent(&out, bytes.TrimSpace(line), "", "  ") != nil {
		return string(line)
	}
	return out.String()
}

func usageDetail(u Usage) string {
	cost := "not priced"
	if u.Priced {
		cost = fmt.Sprintf("%s at API list prices (%s)", Dollars(u.CostUSD), ClaudePricesAsOf)
	}
	return fmt.Sprintf("model        %s\ninput        %d\n  cache read %d\n  cache write %d\noutput       %d\n  reasoning  %d\nduration     %s\ncost         %s",
		u.Model, u.Input, u.CacheRead, u.CacheWrite, u.Output, u.Reasoning, u.Duration.Round(time.Millisecond), cost)
}
