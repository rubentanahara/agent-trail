package tui

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func (m model) parentLabel(session trail.Session) string {
	parent, ok := m.sessions[session.Parent]
	if !ok {
		return ""
	}
	return sessionLabel(parent)
}

func (m model) agentsLine(key string) string {
	agents := m.children[key]
	if len(agents) == 0 {
		return ""
	}
	running := 0
	for _, agent := range agents {
		if m.sessions[agent].Liveness == trail.LivenessRunning {
			running++
		}
	}
	return fmt.Sprintf("%d (%d running) · with agents %s", len(agents), running, costLine(m.familySession(key)))
}

func sessionMetric(session trail.Session) string {
	if session.CLI == trail.Copilot && session.Billing != trail.BillingAPI && session.PremiumRequests > 0 {
		return fmt.Sprintf("%g req", session.PremiumRequests)
	}
	if session.PricedCalls == 0 {
		return trail.Tokens(session.Tokens.Output) + " out"
	}
	if session.Billing == trail.BillingAPI {
		return apiStyle.Render(trail.Dollars(session.Tokens.CostUSD) + " API")
	}
	return "~" + trail.Dollars(session.Tokens.CostUSD)
}

func stateLine(session trail.Session) string {
	activity := optional(" · ", session.Activity)
	switch session.Liveness {
	case trail.LivenessRunning:
		return runningStyle.Render("running"+activity) + optional(" · pid ", pidText(session.PID))
	case trail.LivenessClosed:
		return "closed"
	}
	return "open or closed not recorded by " + string(session.CLI) + activity
}

func pidText(pid int) string {
	if pid <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", pid)
}

func (m model) infoTitle() string {
	if session, ok := m.sessions[m.sessionKeyAt(m.sessionCursor)]; ok {
		return "Session " + shortID(session.ID)
	}
	return "Totals"
}

func (m model) infoRows() []string {
	session, ok := m.sessions[m.sessionKeyAt(m.sessionCursor)]
	if !ok {
		return m.totalRows()
	}
	rows := []string{
		field("Name", sessionLabel(session)),
		field("State", stateLine(session)),
		field("CLI", string(session.CLI)+optional(" · agent ", session.Agent)),
		field("Parent", m.parentLabel(session)),
		field("Agents", m.agentsLine(session.Key)),
		field("Subagents", strings.Join(session.Subagents, ", ")),
		field("Folder", session.Cwd),
		field("Model", session.Model),
		field("Branch", session.Branch),
		field("Mode", session.Permission),
		field("Billing", billingLabel(session)),
		field("Active", activeSpan(session)),
		field("Tokens", tokenLine(session.Tokens)),
		field("Cost", costLine(session)),
		field("Reported", reportedLine(session)),
		field("Limits", limitsLine(session)),
		field("Lines", linesLine(session)),
		field("Events", fmt.Sprintf("%d · %d model calls", session.Events, session.Calls)),
	}
	return nonEmpty(rows)
}

type sessionTotals struct {
	perCLI         map[trail.CLI]int
	tokens         trail.Usage
	apiCost        float64
	equivalentCost float64
	premium        float64
}

func (m model) sumSessions() sessionTotals {
	totals := sessionTotals{perCLI: map[trail.CLI]int{}}
	for _, key := range m.order {
		if m.isChild(m.sessions[key]) {
			continue
		}
		session := m.familySession(key)
		totals.perCLI[session.CLI]++
		totals.tokens.Input += session.Tokens.Input
		totals.tokens.CacheRead += session.Tokens.CacheRead
		totals.tokens.CacheWrite += session.Tokens.CacheWrite
		totals.tokens.Output += session.Tokens.Output
		totals.premium += session.PremiumRequests
		if session.Billing == trail.BillingAPI {
			totals.apiCost += session.Tokens.CostUSD
		} else {
			totals.equivalentCost += session.Tokens.CostUSD
		}
	}
	return totals
}

func (m model) totalRows() []string {
	totals := m.sumSessions()
	return nonEmpty([]string{
		field("Repo", cmp.Or(m.context.Repo, everyRepositoryLabel)),
		field("Running", fmt.Sprintf("%d sessions", m.runningCount())),
		field("Sessions", fmt.Sprintf("claude %d · codex %d · copilot %d", totals.perCLI[trail.Claude], totals.perCLI[trail.Codex], totals.perCLI[trail.Copilot])),
		field("Tokens", tokenLine(totals.tokens)),
		field("API billed", nonZeroDollars(totals.apiCost)),
		field("Subscription", nonZeroDollars(totals.equivalentCost)+optional("", ifPositive(totals.equivalentCost, " API-equivalent"))),
		field("Copilot", ifPositive(totals.premium, fmt.Sprintf("%g premium requests", totals.premium))),
		field("Events", fmt.Sprintf("%d", len(m.events))),
		field("Prices", m.pricesLine()),
		field("Legend", strings.TrimSpace(runningGlyph+"running "+closedGlyph+"closed "+unknownGlyph+"not recorded")),
	})
}

func (m model) pricesLine() string {
	if m.context.PriceWarning != "" {
		return failedStyle.Render(m.context.PriceWarning)
	}
	if m.context.PricesAsOf == "" {
		return failedStyle.Render("none loaded, costs not shown")
	}
	return faintStyle.Render("LiteLLM list, " + m.context.PricesAsOf)
}

func reportedLine(session trail.Session) string {
	if !session.HasReportedCost {
		return ""
	}
	return trail.Dollars(session.ReportedCostUSD) + " by Claude Code"
}

func field(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return labelStyle.Render(fmt.Sprintf("%-9s", label)) + " " + value
}

func nonEmpty(rows []string) []string {
	out := rows[:0]
	for _, row := range rows {
		if row != "" {
			out = append(out, row)
		}
	}
	return out
}

func optional(prefix, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func ifPositive(amount float64, text string) string {
	if amount <= 0 {
		return ""
	}
	return text
}

func nonZeroDollars(amount float64) string {
	return ifPositive(amount, trail.Dollars(amount))
}

func billingLabel(session trail.Session) string {
	switch session.Billing {
	case trail.BillingAPI:
		return apiStyle.Render("API key" + optional(" · ", session.Plan))
	case trail.BillingSubscription:
		return "subscription" + optional(" · ", session.Plan)
	}
	return billingNotRecorded
}

func activeSpan(session trail.Session) string {
	if session.Start.IsZero() {
		return ""
	}
	span := strings.TrimSuffix(session.Last.Sub(session.Start).Round(time.Minute).String(), "0s")
	return fmt.Sprintf("%s → %s (%s)", session.Start.Local().Format("01-02 15:04"), session.Last.Local().Format("15:04"), cmp.Or(span, "<1m"))
}

func tokenLine(u trail.Usage) string {
	if u.Input == 0 && u.Output == 0 {
		return ""
	}
	return fmt.Sprintf("in %s (cache %s/%s) · out %s", trail.Tokens(u.Input), trail.Tokens(u.CacheRead), trail.Tokens(u.CacheWrite), trail.Tokens(u.Output)) +
		optional(" · think ", tokensIfPositive(u.Reasoning))
}

func tokensIfPositive(n int64) string {
	if n <= 0 {
		return ""
	}
	return trail.Tokens(n)
}

func costLine(session trail.Session) string {
	if session.PricedCalls == 0 {
		return ""
	}
	cost := trail.Dollars(session.Tokens.CostUSD)
	if session.PricedCalls < session.Calls {
		cost += fmt.Sprintf(" (%d calls unpriced)", session.Calls-session.PricedCalls)
	}
	if session.Billing == trail.BillingAPI {
		return apiStyle.Render(cost + " billed to API key")
	}
	return "~" + cost + " API-equivalent"
}

func limitsLine(session trail.Session) string {
	var parts []string
	if session.HasRateLimit {
		parts = append(parts, fmt.Sprintf("rate limit %.0f%% used", session.RateLimitPercent))
	}
	if session.HasContext {
		parts = append(parts, fmt.Sprintf("context %.0f%%", session.ContextPercent))
	}
	if session.PremiumRequests > 0 {
		parts = append(parts, fmt.Sprintf("%g premium requests", session.PremiumRequests))
	}
	return strings.Join(parts, " · ")
}

func linesLine(session trail.Session) string {
	if session.LinesAdded == 0 && session.LinesRemoved == 0 {
		return ""
	}
	return fmt.Sprintf("+%d −%d", session.LinesAdded, session.LinesRemoved)
}
