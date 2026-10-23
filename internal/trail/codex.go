package trail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	codexIDLength       = 36
	maxIndexLine        = 1 << 20
	codexUserThread     = "user"
	codexExecSource     = "exec"
	percentScale        = 100
	codexRolloutPattern = "rollout-*.jsonl"
)

var codexTurnEvents = map[string]string{"task_started": "started", "task_complete": "complete"}

var codexHandledRecords = map[string]bool{"session_meta": true, "turn_context": true, "event_msg": true, "token_usage_record": true}

func codexHandledEvent(eventType string) bool {
	_, turn := codexTurnEvents[eventType]
	return turn || eventType == "item_completed" || eventType == "token_count" ||
		strings.Contains(eventType, "error") || strings.Contains(eventType, "aborted")
}

type codexRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	Type           string          `json:"type"`
	CLIVersion     string          `json:"cli_version"`
	Cwd            string          `json:"cwd"`
	ThreadSource   string          `json:"thread_source"`
	Source         json.RawMessage `json:"source"`
	Git            *codexGit       `json:"git"`
	Model          string          `json:"model"`
	ApprovalPolicy string          `json:"approval_policy"`
	SandboxPolicy  json.RawMessage `json:"sandbox_policy"`
	Item           json.RawMessage `json:"item"`
	Info           *codexInfo      `json:"info"`
	RateLimits     *codexLimits    `json:"rate_limits"`
	Usage          *codexTokens    `json:"usage"`
	ResponseID     string          `json:"response_id"`
}

type codexGit struct {
	Branch string `json:"branch"`
}

type codexItem struct {
	Type             string                     `json:"type"`
	Content          json.RawMessage            `json:"content"`
	Command          json.RawMessage            `json:"command"`
	AggregatedOutput string                     `json:"aggregated_output"`
	ExitCode         *int                       `json:"exit_code"`
	Status           string                     `json:"status"`
	Changes          map[string]codexFileChange `json:"changes"`
}

type codexFileChange struct {
	Type        string `json:"type"`
	Content     string `json:"content"`
	UnifiedDiff string `json:"unified_diff"`
}

type codexInfo struct {
	LastTokenUsage     *codexTokens `json:"last_token_usage"`
	ModelContextWindow int64        `json:"model_context_window"`
}

type codexLimits struct {
	PlanType *string `json:"plan_type"`
	Primary  *struct {
		UsedPercent float64 `json:"used_percent"`
	} `json:"primary"`
}

type codexTokens struct {
	InputTokens     int64 `json:"input_tokens"`
	CachedTokens    int64 `json:"cached_input_tokens"`
	CacheWrite      int64 `json:"cache_write_input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_output_tokens"`
}

type codexParser struct {
	model         string
	seenResponses map[string]bool
}

func newCodexParser(string) parser {
	return &codexParser{seenResponses: map[string]bool{}}
}

func codexArtifacts(path string) []string {
	return []string{path}
}

func codexSessionID(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(stem) < codexIDLength {
		return stem
	}
	return stem[len(stem)-codexIDLength:]
}

func (p *codexParser) parse(raw []byte, session *Session) ([]Event, error) {
	var record codexRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if !codexHandledRecords[record.Type] {
		return nil, nil
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if record.Type == "event_msg" {
		if err := json.Unmarshal(record.Payload, &envelope); err != nil {
			return nil, err
		}
		if !codexHandledEvent(envelope.Type) {
			return nil, nil
		}
	}
	var payload codexPayload
	if len(record.Payload) > 0 {
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return nil, err
		}
	}
	p.updateSession(record.Type, payload, session)
	at, ok := parseTime(record.Timestamp)
	if !ok {
		return nil, nil
	}
	events := p.events(record.Type, payload, record.Payload)
	for i := range events {
		events[i].Time, events[i].CLI = at, Codex
	}
	return events, nil
}

func (p *codexParser) updateSession(recordType string, payload codexPayload, session *Session) {
	switch recordType {
	case "session_meta":
		session.Cwd = payload.Cwd
		if payload.Git != nil {
			session.setBranch(payload.Git.Branch)
		}
		if payload.ThreadSource != "" && payload.ThreadSource != codexUserThread {
			session.Agent = payload.ThreadSource
		}
		session.Automated = rawText(payload.Source) == codexExecSource
	case "turn_context":
		p.model = payload.Model
		session.Model = payload.Model
		session.Permission = strings.Trim(payload.ApprovalPolicy+" / "+codexSandbox(payload.SandboxPolicy), " /")
	case "event_msg":
		codexTurnUpdate(payload.Type, session)
		if payload.Type == "token_count" {
			codexLimitsUpdate(payload, session)
		}
	}
}

func codexTurnUpdate(eventType string, session *Session) {
	switch {
	case eventType == "task_started":
		session.TurnOpen = true
	case eventType == "task_complete" || strings.Contains(eventType, "aborted"):
		session.TurnOpen = false
	}
}

func codexSandbox(raw json.RawMessage) string {
	var policy struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &policy) == nil {
		return policy.Type
	}
	return rawText(raw)
}

func codexLimitsUpdate(payload codexPayload, session *Session) {
	if limits := payload.RateLimits; limits != nil {
		session.Billing, session.Plan = BillingAPI, ""
		if limits.PlanType != nil {
			session.Billing, session.Plan = BillingSubscription, *limits.PlanType
		}
		if limits.Primary != nil {
			session.RateLimitPercent, session.HasRateLimit = limits.Primary.UsedPercent, true
		}
	}
	if info := payload.Info; info != nil && info.LastTokenUsage != nil && info.ModelContextWindow > 0 {
		session.ContextPercent = percentScale * float64(info.LastTokenUsage.InputTokens) / float64(info.ModelContextWindow)
		session.HasContext = true
	}
}

func (p *codexParser) events(recordType string, payload codexPayload, rawPayload json.RawMessage) []Event {
	switch recordType {
	case "session_meta":
		return []Event{{Kind: KindSession, Text: "start " + payload.CLIVersion}}
	case "token_usage_record":
		return p.usageEvents(payload)
	case "event_msg":
		return codexEventMessage(payload, rawPayload)
	}
	return nil
}

func (p *codexParser) usageEvents(payload codexPayload) []Event {
	if payload.Usage == nil {
		return nil
	}
	if payload.ResponseID != "" {
		if p.seenResponses[payload.ResponseID] {
			return nil
		}
		p.seenResponses[payload.ResponseID] = true
	}
	tokens := payload.Usage
	usage := Usage{
		Model:      p.model,
		Input:      tokens.InputTokens,
		CacheRead:  tokens.CachedTokens,
		CacheWrite: tokens.CacheWrite,
		Output:     tokens.OutputTokens,
		Reasoning:  tokens.ReasoningTokens,
	}
	return []Event{{Kind: KindUsage, Usage: &usage}}
}

func codexEventMessage(payload codexPayload, rawPayload json.RawMessage) []Event {
	if payload.Type == "item_completed" {
		return codexItemEvents(payload.Item)
	}
	if turn, ok := codexTurnEvents[payload.Type]; ok {
		return []Event{{Kind: KindTurn, Text: turn}}
	}
	if strings.Contains(payload.Type, "error") || strings.Contains(payload.Type, "aborted") {
		return []Event{{Kind: KindError, Text: compactJSON(rawPayload)}}
	}
	return nil
}

func codexItemEvents(raw json.RawMessage) []Event {
	var item codexItem
	if json.Unmarshal(raw, &item) != nil {
		return []Event{{Kind: KindUnparsed, Text: compactJSON(raw)}}
	}
	switch item.Type {
	case "UserMessage":
		return []Event{{Kind: KindUser, Text: joinedText(item.Content)}}
	case "AgentMessage":
		return []Event{{Kind: KindAssistant, Text: joinedText(item.Content)}}
	case "CommandExecution":
		return []Event{
			{Kind: KindTool, Text: "exec " + codexCommand(item.Command)},
			{Kind: KindResult, Text: codexCommandStatus(item) + item.AggregatedOutput},
		}
	case "FileChange":
		return []Event{{Kind: KindEdit, Text: codexChanges(item.Changes), Files: slices.Collect(maps.Keys(item.Changes)), patches: codexPatches(item.Changes)}}
	}
	kind := Kind(item.Type)
	if kind == "" {
		kind = "item"
	}
	return []Event{{Kind: kind, Text: compactJSON(raw)}}
}

func codexCommand(raw json.RawMessage) string {
	var parts []string
	if json.Unmarshal(raw, &parts) == nil && len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return rawText(raw)
}

func codexCommandStatus(item codexItem) string {
	if item.ExitCode != nil && *item.ExitCode != 0 {
		return fmt.Sprintf(FailedPrefix+"exit %d ", *item.ExitCode)
	}
	if item.ExitCode == nil && item.Status != "completed" {
		status := item.Status
		if status == "" {
			status = "no exit code"
		}
		return FailedPrefix + status + " "
	}
	return ""
}

func codexChanges(changes map[string]codexFileChange) string {
	parts := make([]string, 0, len(changes))
	for path, change := range changes {
		parts = append(parts, change.Type+" "+path)
	}
	return strings.Join(parts, ", ")
}

func codexBelongs(path, repo string) (belongs, decided bool) {
	file, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer file.Close()
	first, err := bufio.NewReader(file).ReadBytes('\n')
	if err != nil {
		return false, false
	}
	var record struct {
		Payload struct {
			Cwd *string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &record) != nil {
		return false, false
	}
	return record.Payload.Cwd != nil && underRepo(*record.Payload.Cwd, repo), true
}

func codexCandidates(home string) []string {
	var files []string
	root := filepath.Join(home, ".codex", "sessions")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if matched, _ := filepath.Match(codexRolloutPattern, entry.Name()); matched && !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func codexThreadNames(home string) map[string]string {
	names := map[string]string{}
	file, err := os.Open(codexIndexPath(home))
	if err != nil {
		return names
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, maxIndexLine)
	for scanner.Scan() {
		var entry struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.ThreadName != "" {
			names[entry.ID] = entry.ThreadName
		}
	}
	return names
}

func codexIndexPath(home string) string {
	return filepath.Join(home, ".codex", "session_index.jsonl")
}
