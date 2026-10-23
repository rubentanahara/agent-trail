package trail

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

type parser interface {
	parse(raw []byte, session *Session) ([]Event, error)
}

type source struct {
	cli        CLI
	candidates func() []string
	belongs    func(path, repo string) (belongs, decided bool)
	sessionID  func(path string) string
	parentID   func(path string) string
	artifacts  func(path string) []string
}

var newParsers = map[CLI]func(path string) parser{Claude: newClaudeParser, Codex: newCodexParser, Copilot: newCopilotParser}

func sources(home, repo string) []source {
	return []source{
		{Claude, func() []string { return claudeCandidates(home, repo) }, claudeBelongs, claudeSessionID, claudeParentID, claudeArtifacts},
		{Codex, func() []string { return codexCandidates(home) }, codexBelongs, codexSessionID, noParent, codexArtifacts},
		{Copilot, func() []string { return copilotCandidates(home) }, copilotBelongs, copilotSessionID, noParent, copilotArtifacts},
	}
}

func noParent(string) string {
	return ""
}

type transcript struct {
	source     *source
	parser     parser
	sessionKey string
	parentKey  string
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
	mu          sync.Mutex
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
	prices      pricing.Table
}

const EveryRepository = ""

func New(home, repo string, prices pricing.Table) *Trail {
	return &Trail{
		home:        home,
		repo:        repo,
		prices:      prices,
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
	t.mu.Lock()
	defer t.mu.Unlock()
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
	t.mu.Lock()
	defer t.mu.Unlock()
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
			file.offset, file.hideBefore, file.parser = 0, 0, newParsers[file.source.cli](path)
			events = append(events, t.read(path, file)...)
		case size > file.offset:
			events = append(events, t.read(path, file)...)
		}
	}
	return t.finish(events)
}

const recentWriteGuard = 2 * time.Minute

var (
	ErrSessionRunning        = errors.New("session is running; close it before deleting")
	ErrSessionRecentlyActive = errors.New("session wrote to its transcript in the last 2 minutes; wait before deleting")
)

func (t *Trail) Delete(key string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refreshLiveness()
	family := t.family(key)
	for _, member := range family {
		if err := t.deletable(member); err != nil {
			return err
		}
	}
	for path, file := range t.transcripts {
		if !slices.Contains(family, file.sessionKey) {
			continue
		}
		for _, target := range file.source.artifacts(path) {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
		delete(t.transcripts, path)
	}
	for _, member := range family {
		delete(t.sessions, member)
		delete(t.changed, member)
	}
	return nil
}

func (t *Trail) family(key string) []string {
	members := []string{key}
	for child, session := range t.sessions {
		if session.Parent == key {
			members = append(members, child)
		}
	}
	return members
}

func (t *Trail) deletable(key string) error {
	if session := t.sessions[key]; session != nil && (session.Liveness == LivenessRunning || session.TurnOpen) {
		return ErrSessionRunning
	}
	for path, file := range t.transcripts {
		if file.sessionKey == key && writtenWithin(path, recentWriteGuard) {
			return ErrSessionRecentlyActive
		}
	}
	return nil
}

func writtenWithin(path string, window time.Duration) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < window
}

func (t *Trail) finish(events []Event) Batch {
	t.refreshThreadNames()
	t.refreshLiveness()
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
	t.transcripts[path] = newTranscript(src, path)
	return true
}

func newTranscript(src *source, path string) *transcript {
	file := &transcript{source: src, parser: newParsers[src.cli](path), sessionKey: sessionKey(src.cli, src.sessionID(path))}
	if parent := src.parentID(path); parent != "" {
		file.parentKey = sessionKey(src.cli, parent)
		file.sessionKey = file.parentKey + "/" + src.sessionID(path)
	}
	return file
}

func sessionKey(cli CLI, id string) string {
	return string(cli) + ":" + id
}

func (t *Trail) session(file *transcript, path string) *Session {
	key := file.sessionKey
	session := t.sessions[key]
	if session == nil {
		session = &Session{Key: key, CLI: file.source.cli, ID: file.source.sessionID(path), Parent: file.parentKey}
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
		events[i].Summary = Clip(summaryText(events[i].Text, session.Cwd))
		events[i].Text = ""
		session.touch(events[i].Time)
		if events[i].Kind == KindUser && events[i].Agent == "" {
			session.setFallbackName(events[i].Summary)
		}
		if usage := events[i].Usage; usage != nil {
			usage.CostUSD, usage.Priced = t.prices.Cost(pricing.Tokens{
				Model: usage.Model, Input: usage.Input, CacheRead: usage.CacheRead, CacheWrite: usage.CacheWrite,
				CacheWrite1h: usage.CacheWrite1h, Output: usage.Output, Fast: usage.Fast,
			})
			events[i].Summary = Clip(usage.Line())
			session.addUsage(*usage)
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
	size := fileSize(codexIndexPath(t.home))
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
