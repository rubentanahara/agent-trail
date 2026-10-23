package trail

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	claudeOAuthBeta       = "oauth-2025-04-20"
	claudeSubagentDir     = "subagents"
	claudeMetaSuffix      = ".meta.json"
	claudeAgentFilePrefix = "agent-"
	claudeUnknownAgent    = "sub"
	nameRankAITitle       = 1
	nameRankAgentName     = 2
	nameRankCustomTitle   = 3
	claudeQueuedCommand   = "queued_command"
	claudeHumanOrigin     = "human"
	claudeSpeedFast       = "fast"
	claudeSDKEntrypoint   = "sdk"
	claudeCommandPrefix   = "<command-name>"
	claudeBashPrefix      = "<bash-input>"
	claudeInjectedPrefix  = "<"
	HiddenThinking        = "(thinking text not returned by the API)"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]`)

var claudeHandledTypes = map[string]bool{
	"system": true, "attachment": true, "user": true, "assistant": true,
	"agent-setting": true, "permission-mode": true, "custom-title": true, "agent-name": true,
	"ai-title": true, "api-request": true, "cost-state": true,
}

type claudeEntry struct {
	Type           string             `json:"type"`
	Timestamp      string             `json:"timestamp"`
	IsSidechain    bool               `json:"isSidechain"`
	IsMeta         bool               `json:"isMeta"`
	Subtype        string             `json:"subtype"`
	Content        json.RawMessage    `json:"content"`
	Attachment     *claudeAttachment  `json:"attachment"`
	Message        *claudeMessage     `json:"message"`
	Cwd            string             `json:"cwd"`
	Entrypoint     string             `json:"entrypoint"`
	GitBranch      string             `json:"gitBranch"`
	PermissionMode string             `json:"permissionMode"`
	AgentSetting   string             `json:"agentSetting"`
	CustomTitle    string             `json:"customTitle"`
	AITitle        string             `json:"aiTitle"`
	AgentName      string             `json:"agentName"`
	Params         *claudeRequestBody `json:"params"`
	LinesAdded     int                `json:"totalLinesAdded"`
	TotalCostUSD   *float64           `json:"totalCostUSD"`
	LinesRemoved   int                `json:"totalLinesRemoved"`
}

type claudeAttachment struct {
	Type     string          `json:"type"`
	HookName string          `json:"hookName"`
	Prompt   json.RawMessage `json:"prompt"`
	Origin   struct {
		Kind string `json:"kind"`
	} `json:"origin"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheReadTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_input_tokens"`
	Speed               string `json:"speed"`
	CacheCreation       *struct {
		OneHour  int64 `json:"ephemeral_1h_input_tokens"`
		FiveMins int64 `json:"ephemeral_5m_input_tokens"`
	} `json:"cache_creation"`
	OutputDetails *struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type claudeRequestBody struct {
	Betas []string `json:"betas"`
}

type claudeBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}

type claudeParser struct {
	agent        string
	description  string
	finished     bool
	metaPath     string
	seenMessages map[string]bool
}

type claudeAgentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Finished    bool   `json:"finished"`
}

func newClaudeParser(path string) parser {
	parser := &claudeParser{seenMessages: map[string]bool{}}
	if !isClaudeSubagent(path) {
		return parser
	}
	parser.metaPath = claudeMetaPath(path)
	meta := readClaudeAgentMeta(parser.metaPath)
	parser.agent, parser.description, parser.finished = cmp.Or(meta.AgentType, claudeUnknownAgent), meta.Description, meta.Finished
	return parser
}

func isClaudeSubagent(path string) bool {
	return filepath.Base(filepath.Dir(path)) == claudeSubagentDir
}

func claudeMetaPath(path string) string {
	return strings.TrimSuffix(path, ".jsonl") + claudeMetaSuffix
}

func readClaudeAgentMeta(path string) claudeAgentMeta {
	var meta claudeAgentMeta
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func claudeSessionID(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if isClaudeSubagent(path) {
		return strings.TrimPrefix(stem, claudeAgentFilePrefix)
	}
	return stem
}

func claudeParentID(path string) string {
	if !isClaudeSubagent(path) {
		return ""
	}
	return filepath.Base(filepath.Dir(filepath.Dir(path)))
}

func claudeArtifacts(path string) []string {
	if isClaudeSubagent(path) {
		return []string{path, claudeMetaPath(path)}
	}
	return []string{path, strings.TrimSuffix(path, ".jsonl")}
}

func claudeProjectPrefix(repo string) string {
	return nonAlphanumeric.ReplaceAllString(repo, "-")
}

func (p *claudeParser) parse(raw []byte, session *Session) ([]Event, error) {
	var entry claudeEntry
	err := json.Unmarshal(raw, &entry)
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return nil, err
	}
	if !claudeHandledTypes[entry.Type] {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.updateSession(entry, session)
	at, ok := parseTime(entry.Timestamp)
	if !ok {
		return nil, nil
	}
	events := p.events(entry)
	if usage, ok := p.usageEvent(entry); ok {
		events = append(events, usage)
	}
	for i := range events {
		events[i].Time, events[i].CLI = at, Claude
	}
	return events, nil
}

func (p *claudeParser) updateSession(entry claudeEntry, session *Session) {
	if p.agent != "" {
		session.Agent, session.metaPath = p.agent, p.metaPath
		session.Finished = session.Finished || p.finished
		session.setName(cmp.Or(p.description, p.agent), nameRankCustomTitle)
	}
	switch entry.Type {
	case "agent-setting":
		if p.agent == "" {
			session.Agent = entry.AgentSetting
		}
	case "permission-mode":
		if p.agent == "" {
			session.Permission = entry.PermissionMode
		}
	case "custom-title":
		session.setName(entry.CustomTitle, nameRankCustomTitle)
	case "agent-name":
		session.setName(entry.AgentName, nameRankAgentName)
	case "ai-title":
		session.setName(entry.AITitle, nameRankAITitle)
	case "api-request":
		session.Billing = claudeBilling(entry.Params)
	case "cost-state":
		session.LinesAdded, session.LinesRemoved = entry.LinesAdded, entry.LinesRemoved
		if entry.TotalCostUSD != nil {
			session.ReportedCostUSD, session.HasReportedCost = *entry.TotalCostUSD, true
		}
	}
	if entry.Cwd != "" {
		session.Cwd = entry.Cwd
	}
	if entry.GitBranch != "" {
		session.Branch = entry.GitBranch
	}
	if strings.HasPrefix(entry.Entrypoint, claudeSDKEntrypoint) {
		session.Automated = true
	}
	if entry.Type == "assistant" && entry.Message != nil && entry.Message.Model != "" {
		session.Model = entry.Message.Model
	}
}

func claudeBilling(params *claudeRequestBody) Billing {
	if params != nil && slices.Contains(params.Betas, claudeOAuthBeta) {
		return BillingSubscription
	}
	return BillingAPI
}

func (p *claudeParser) who(entry claudeEntry) string {
	if !entry.IsSidechain {
		return ""
	}
	if p.agent == "" {
		return claudeUnknownAgent
	}
	return p.agent
}

func (p *claudeParser) events(entry claudeEntry) []Event {
	agent := p.who(entry)
	switch entry.Type {
	case "system":
		return []Event{{Agent: agent, Kind: KindSystem, Text: entry.Subtype + " " + Clip(rawText(entry.Content))}}
	case "attachment":
		return claudeAttachmentEvents(agent, entry.Attachment)
	case "user", "assistant":
		return claudeMessageEvents(agent, entry)
	}
	return nil
}

func claudeAttachmentEvents(agent string, attachment *claudeAttachment) []Event {
	if attachment == nil {
		attachment = &claudeAttachment{}
	}
	if attachment.Type != claudeQueuedCommand {
		return []Event{{Agent: agent, Kind: KindAttachment, Text: attachment.Type + " " + attachment.HookName}}
	}
	kind := KindContext
	if attachment.Origin.Kind == claudeHumanOrigin {
		kind = KindUser
	}
	return []Event{{Agent: agent, Kind: kind, Text: rawText(attachment.Prompt)}}
}

func claudeTypedByHuman(entry claudeEntry, content string, isString bool) bool {
	if entry.IsMeta || entry.IsSidechain {
		return false
	}
	if !isString || !strings.HasPrefix(content, claudeInjectedPrefix) {
		return true
	}
	return strings.HasPrefix(content, claudeCommandPrefix) || strings.HasPrefix(content, claudeBashPrefix)
}

func claudeMessageEvents(agent string, entry claudeEntry) []Event {
	if entry.Message == nil {
		return nil
	}
	var text string
	isString := json.Unmarshal(entry.Message.Content, &text) == nil
	speaker := KindAssistant
	if entry.Type == "user" {
		speaker = KindContext
		if claudeTypedByHuman(entry, text, isString) {
			speaker = KindUser
		}
	}
	if isString {
		return []Event{{Agent: agent, Kind: speaker, Text: text}}
	}
	var blocks []claudeBlock
	if json.Unmarshal(entry.Message.Content, &blocks) != nil {
		return nil
	}
	events := make([]Event, 0, len(blocks))
	for _, block := range blocks {
		if event, ok := claudeBlockEvent(agent, speaker, block); ok {
			events = append(events, event)
		}
	}
	return events
}

func claudeBlockEvent(agent string, speaker Kind, block claudeBlock) (Event, bool) {
	switch block.Type {
	case "text":
		return Event{Agent: agent, Kind: speaker, Text: block.Text}, true
	case "thinking":
		return Event{Agent: agent, Kind: KindThinking, Text: cmp.Or(block.Thinking, HiddenThinking)}, true
	case "tool_use":
		return Event{Agent: agent, Kind: KindTool, Text: block.Name + " " + argumentsText(block.Input), Files: pathArguments(block.Input)}, true
	case "tool_result":
		text := rawText(block.Content)
		if len(block.Content) > 0 && block.Content[0] == '[' {
			text = joinedText(block.Content)
		}
		return Event{Agent: agent, Kind: KindResult, Text: failedPrefix(block.IsError) + text}, true
	}
	return Event{}, false
}

func (p *claudeParser) usageEvent(entry claudeEntry) (Event, bool) {
	if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
		return Event{}, false
	}
	if id := entry.Message.ID; id != "" {
		if p.seenMessages[id] {
			return Event{}, false
		}
		p.seenMessages[id] = true
	}
	usage := claudeUsageOf(entry.Message.Model, entry.Message.Usage)
	return Event{Agent: p.who(entry), Kind: KindUsage, Usage: &usage}, true
}

func claudeUsageOf(model string, raw *claudeUsage) Usage {
	usage := Usage{
		Model:      model,
		Input:      raw.InputTokens + raw.CacheReadTokens + raw.CacheCreationTokens,
		CacheRead:  raw.CacheReadTokens,
		CacheWrite: raw.CacheCreationTokens,
		Output:     raw.OutputTokens,
		Fast:       raw.Speed == claudeSpeedFast,
	}
	if raw.CacheCreation != nil {
		usage.CacheWrite1h = raw.CacheCreation.OneHour
	}
	if raw.OutputDetails != nil {
		usage.Reasoning = raw.OutputDetails.ThinkingTokens
	}
	return usage
}

func claudeBelongs(path, repo string) (belongs, decided bool) {
	file, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return false, false
		}
		if !strings.Contains(string(line), `"cwd"`) {
			continue
		}
		var entry struct {
			Cwd *string `json:"cwd"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Cwd != nil {
			return underRepo(*entry.Cwd, repo), true
		}
	}
}

func underRepo(path, repo string) bool {
	return repo == EveryRepository || path == repo || strings.HasPrefix(path, repo+"/")
}

func claudeCandidates(home, repo string) []string {
	projects, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", claudeProjectPrefix(repo)+"*"))
	var files []string
	for _, project := range projects {
		mains, _ := filepath.Glob(filepath.Join(project, "*.jsonl"))
		subagents, _ := filepath.Glob(filepath.Join(project, "*", claudeSubagentDir, "*.jsonl"))
		files = append(files, mains...)
		files = append(files, subagents...)
	}
	return files
}
