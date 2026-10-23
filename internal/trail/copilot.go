package trail

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	copilotEventsFile    = "events.jsonl"
	copilotWorkspaceFile = "workspace.yaml"
	copilotPlan          = "copilot"
	copilotBYOKPlan      = "byok"
)

var copilotHandledTypes = map[string]bool{
	"user.message": true, "assistant.message": true, "tool.execution_start": true, "tool.execution_complete": true,
	"permission.requested": true, "permission.completed": true, "hook.end": true, "abort": true,
	"session.start": true, "session.shutdown": true, "session.error": true, "session.info": true,
	"session.model_change": true, "session.usage_record": true, "session.usage_checkpoint": true,
	"subagent.selected": true, "subagent.started": true, "subagent.completed": true, "subagent.failed": true,
}

type copilotRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type copilotData struct {
	Content           json.RawMessage     `json:"content"`
	ToolName          string              `json:"toolName"`
	Arguments         json.RawMessage     `json:"arguments"`
	Success           bool                `json:"success"`
	Result            json.RawMessage     `json:"result"`
	Error             json.RawMessage     `json:"error"`
	PermissionRequest *copilotPermission  `json:"permissionRequest"`
	HookType          string              `json:"hookType"`
	SelectedModel     string              `json:"selectedModel"`
	NewModel          string              `json:"newModel"`
	Context           *copilotContext     `json:"context"`
	Reason            string              `json:"reason"`
	Message           json.RawMessage     `json:"message"`
	Usage             *copilotUsage       `json:"usage"`
	PremiumRequests   *float64            `json:"totalPremiumRequests"`
	CodeChanges       *copilotCodeChanges `json:"codeChanges"`
	AgentName         string              `json:"agentName"`
	AgentDisplayName  string              `json:"agentDisplayName"`
}

type copilotPermission struct {
	Kind            string `json:"kind"`
	Intention       string `json:"intention"`
	FullCommandText string `json:"fullCommandText"`
	FileName        string `json:"fileName"`
}

type copilotContext struct {
	Cwd    string `json:"cwd"`
	Branch string `json:"branch"`
}

type copilotUsage struct {
	Model           string  `json:"model"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	CacheReadTokens int64   `json:"cacheReadTokens"`
	CacheWriteToken int64   `json:"cacheWriteTokens"`
	ReasoningTokens int64   `json:"reasoningTokens"`
	DurationMs      float64 `json:"duration"`
	IsBYOK          bool    `json:"isByok"`
	MaxPromptTokens int64   `json:"maxPromptTokens"`
}

type copilotCodeChanges struct {
	LinesAdded   int `json:"linesAdded"`
	LinesRemoved int `json:"linesRemoved"`
}

type copilotParser struct{}

func newCopilotParser(string) parser {
	return copilotParser{}
}

func copilotSessionID(path string) string {
	return filepath.Base(filepath.Dir(path))
}

func (copilotParser) parse(raw []byte, session *Session) ([]Event, error) {
	var record copilotRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if !copilotHandledTypes[record.Type] {
		return nil, nil
	}
	var data copilotData
	if len(record.Data) > 0 {
		if err := json.Unmarshal(record.Data, &data); err != nil {
			return nil, err
		}
	}
	copilotUpdateSession(record.Type, data, session)
	at, ok := parseTime(record.Timestamp)
	if !ok {
		return nil, nil
	}
	events := copilotEvents(record.Type, data)
	for i := range events {
		events[i].Time, events[i].CLI = at, Copilot
	}
	return events, nil
}

func copilotUpdateSession(eventType string, data copilotData, session *Session) {
	switch eventType {
	case "session.start":
		session.Model = data.SelectedModel
		if data.Context != nil {
			session.Cwd, session.Branch = data.Context.Cwd, data.Context.Branch
		}
	case "session.model_change":
		session.Model = data.NewModel
	case "session.usage_record":
		copilotUsageUpdate(data.Usage, session)
	case "subagent.selected":
		session.Agent = firstNonEmpty(data.AgentDisplayName, data.AgentName)
	case "subagent.started":
		session.addSubagent(firstNonEmpty(data.AgentDisplayName, data.AgentName))
	}
	if data.PremiumRequests != nil {
		session.PremiumRequests = *data.PremiumRequests
	}
	if data.CodeChanges != nil {
		session.LinesAdded, session.LinesRemoved = data.CodeChanges.LinesAdded, data.CodeChanges.LinesRemoved
	}
}

func copilotUsageUpdate(usage *copilotUsage, session *Session) {
	if usage == nil {
		return
	}
	session.Billing, session.Plan = BillingSubscription, copilotPlan
	if usage.IsBYOK {
		session.Billing, session.Plan = BillingAPI, copilotBYOKPlan
	}
	if usage.MaxPromptTokens > 0 {
		session.ContextPercent = percentScale * float64(usage.InputTokens) / float64(usage.MaxPromptTokens)
		session.HasContext = true
	}
}

func copilotEvents(eventType string, data copilotData) []Event {
	kind, text := copilotKindAndText(eventType, data)
	if text == "" {
		return nil
	}
	event := Event{Kind: kind, Text: text}
	if eventType == "session.usage_record" && data.Usage != nil {
		usage := copilotUsageOf(*data.Usage)
		event.Usage, event.Text = &usage, usage.Line()
	}
	return []Event{event}
}

func copilotKindAndText(eventType string, data copilotData) (Kind, string) {
	switch eventType {
	case "user.message":
		return KindUser, rawText(data.Content)
	case "assistant.message":
		return KindAssistant, rawText(data.Content)
	case "tool.execution_start":
		return KindTool, data.ToolName + " " + argumentsText(data.Arguments)
	case "tool.execution_complete":
		return KindResult, copilotResultText(data)
	case "permission.requested":
		return KindPermission, copilotPermissionText(data.PermissionRequest)
	case "permission.completed":
		return KindPermission, copilotDecision(data.Result)
	case "hook.end":
		return KindHook, data.HookType + " " + okOrFailed(data.Success)
	case "session.start":
		return KindSession, "start " + data.SelectedModel
	case "session.shutdown":
		return KindSession, "shutdown"
	case "session.error", "session.info":
		return KindSession, rawText(data.Message)
	case "abort":
		return KindSession, "abort " + data.Reason
	case "session.usage_record":
		return KindUsage, "usage"
	case "subagent.selected", "subagent.started", "subagent.completed", "subagent.failed":
		return KindAgent, strings.TrimPrefix(eventType, "subagent.") + " " + firstNonEmpty(data.AgentDisplayName, data.AgentName)
	}
	return "", ""
}

func copilotUsageOf(raw copilotUsage) Usage {
	return Usage{
		Model:      raw.Model,
		Input:      raw.InputTokens,
		CacheRead:  raw.CacheReadTokens,
		CacheWrite: raw.CacheWriteToken,
		Output:     raw.OutputTokens,
		Reasoning:  raw.ReasoningTokens,
		Duration:   time.Duration(raw.DurationMs * float64(time.Millisecond)),
	}
}

func copilotResultText(data copilotData) string {
	var failure struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data.Error, &failure)
	output := rawText(data.Result)
	var result struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(data.Result, &result) == nil {
		output = rawText(result.Content)
	}
	return failedPrefix(!data.Success) + firstNonEmpty(failure.Message, output)
}

func copilotPermissionText(request *copilotPermission) string {
	if request == nil {
		return " "
	}
	return request.Kind + " " + request.Intention + " " + firstNonEmpty(request.FullCommandText, request.FileName)
}

func copilotDecision(raw json.RawMessage) string {
	var result struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(raw, &result)
	return result.Kind
}

func okOrFailed(success bool) string {
	if success {
		return "ok"
	}
	return "failed"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func copilotWorkspace(eventsPath string) map[string]string {
	fields := map[string]string{}
	file, err := os.Open(filepath.Join(filepath.Dir(eventsPath), copilotWorkspaceFile))
	if err != nil {
		return fields
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ": ")
		if found {
			fields[key] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return fields
}

func copilotBelongs(path, repo string) (belongs, decided bool) {
	fields := copilotWorkspace(path)
	cwd, known := fields["cwd"]
	if !known {
		return false, false
	}
	return underRepo(cwd, repo) || fields["git_root"] == repo, true
}

func copilotCandidates(home string) []string {
	files, _ := filepath.Glob(filepath.Join(home, ".copilot", "session-state", "*", copilotEventsFile))
	return files
}
