package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

type prLineKind int

const (
	textLine prLineKind = iota
	fileHeaderLine
	diffBodyLine
)

type prSource int

const (
	sourceNone prSource = iota
	sourceComment
	sourceThread
	sourceRun
)

type prLine struct {
	text    string
	ref     int
	sub     int
	src     prSource
	kind    prLineKind
	file    int
	newLine int
}

var prTabNames = [prTabCount][2]string{
	{"Overview", "Ovw"},
	{"Checks", "Chk"},
	{"Comments", "Cmt"},
	{"Diff", "Diff"},
	{"Sessions", "Ses"},
}

func plainLines(texts ...string) []prLine {
	lines := make([]prLine, len(texts))
	for i, text := range texts {
		lines[i] = prLine{text: text, ref: -1, sub: -1, file: -1}
	}
	return lines
}

func (m model) prDetailWidth() int {
	_, detailW, _, _ := m.prGeometry()
	return max(1, detailW-2)
}

func (m model) prTabLines() []prLine {
	lines := m.unfilteredTabLines()
	if len(m.pr.detailFilter) == 0 {
		return lines
	}
	kept := lines[:0:0]
	for _, line := range lines {
		if m.pr.detailFilter.matches(strings.ToLower(ansi.Strip(line.text))) {
			kept = append(kept, line)
		}
	}
	return kept
}

func (m model) unfilteredTabLines() []prLine {
	pr, ok := m.pr.selected()
	if !ok {
		return plainLines(faintStyle.Render("No pull request selected"))
	}
	width := m.prDetailWidth()
	switch m.pr.tab {
	case checksTab:
		return m.checkLines(pr)
	case commentsTab:
		return m.commentLines(pr, width)
	case diffTab:
		return m.diffLines(pr)
	case sessionsTab:
		return m.sessionLines(pr)
	}
	return m.overviewLines(pr, width)
}

func (m model) cursorLine() (prLine, bool) {
	lines := m.prTabLines()
	cursor := m.pr.tabCursor[m.pr.tab]
	if cursor < 0 || cursor >= len(lines) {
		return prLine{}, false
	}
	return lines[cursor], true
}

func (m model) overviewLines(pr gh.PR, width int) []prLine {
	lines := plainLines(
		titleStyle.Render(fmt.Sprintf("#%d %s", pr.Number, sanitize(pr.Title))),
		faintStyle.Render(prStateLine(pr)),
		labelStyle.Render("Branch   ")+sanitize(pr.HeadRef)+faintStyle.Render(" → "+sanitize(pr.BaseRef)),
		labelStyle.Render("Review   ")+reviewWords(pr),
		labelStyle.Render("Checks   ")+checkSummary(pr),
		labelStyle.Render("Merge    ")+mergeWords(pr),
		labelStyle.Render("Changes  ")+addedStyle.Render(fmt.Sprintf("+%d", pr.Additions))+" "+failedStyle.Render(fmt.Sprintf("−%d", pr.Deletions))+faintStyle.Render(fmt.Sprintf(" in %d files", len(pr.Files))),
		labelStyle.Render("Sessions ")+m.sessionSummary(pr),
	)
	if len(pr.Labels) > 0 {
		lines = append(lines, plainLines(labelStyle.Render("Labels   ")+sanitize(strings.Join(pr.Labels, ", ")))...)
	}
	lines = append(lines, plainLines("")...)
	return append(lines, plainLines(m.prBody(pr, width)...)...)
}

func (m model) prBody(pr gh.PR, width int) []string {
	if _, loaded := m.pr.details[pr.Number]; !loaded {
		return []string{faintStyle.Render(loadingText)}
	}
	if strings.TrimSpace(pr.Body) == "" {
		return []string{faintStyle.Render("No description provided")}
	}
	return strings.Split(m.markdownIn(m.pr.bodyMarkdown, sanitizeBody(pr.Body), width), "\n")
}

func prStateLine(pr gh.PR) string {
	parts := []string{strings.ToLower(pr.State)}
	if pr.Draft {
		parts = append(parts, "draft")
	}
	parts = append(parts, "by "+sanitize(pr.Author.Login))
	if pr.Author.Bot {
		parts = append(parts, "[bot]")
	}
	if pr.CrossRepo {
		parts = append(parts, "from a fork")
	}
	return strings.Join(parts, " · ")
}

func reviewWords(pr gh.PR) string {
	switch pr.ReviewDecision {
	case "APPROVED":
		return runningStyle.Render("✓ approved")
	case "CHANGES_REQUESTED":
		return failedStyle.Render("✗ changes requested")
	case "REVIEW_REQUIRED":
		return waitingStyle.Render("○ review required")
	}
	return faintStyle.Render("○ no review required")
}

func mergeWords(pr gh.PR) string {
	conflict := pr.Mergeable == "CONFLICTING"
	state := strings.ToLower(strings.ReplaceAll(pr.MergeState, "_", " "))
	if conflict {
		return failedStyle.Render("⚠ conflicts with " + sanitize(pr.BaseRef))
	}
	if state == "" || state == "unknown" {
		return faintStyle.Render("mergeability unknown")
	}
	return "mergeable · " + state
}

func checkCounts(pr gh.PR) (passing, failing, pending, skipped int) {
	for _, check := range pr.Checks {
		switch check.State {
		case gh.CheckPassing:
			passing++
		case gh.CheckFailing:
			failing++
		case gh.CheckPending:
			pending++
		default:
			skipped++
		}
	}
	return
}

func checkSummary(pr gh.PR) string {
	if len(pr.Checks) == 0 {
		return faintStyle.Render("– none reported")
	}
	passing, failing, pending, skipped := checkCounts(pr)
	return fmt.Sprintf("%s passing · %s failing · %s pending · %d skipped",
		runningStyle.Render(strconv.Itoa(passing)), failedStyle.Render(strconv.Itoa(failing)), waitingStyle.Render(strconv.Itoa(pending)), skipped)
}

func (m model) checkLines(pr gh.PR) []prLine {
	lines := plainLines(faintStyle.Render("No checks reported"))
	if len(pr.Checks) > 0 {
		lines = make([]prLine, len(pr.Checks))
		for i, check := range pr.Checks {
			lines[i] = prLine{text: m.checkRow(check), ref: i, sub: -1, file: -1}
		}
	}
	return append(lines, m.runLines()...)
}

func (m model) runLines() []prLine {
	lines := plainLines("", titleStyle.Render("Repository runs")+faintStyle.Render(" · not tied to this pull request"))
	switch {
	case !m.pr.runsLoaded:
		return append(lines, plainLines(faintStyle.Render(loadingText))...)
	case len(m.pr.runs) == 0:
		return append(lines, plainLines(faintStyle.Render("No runs outside pull requests"))...)
	}
	for i, run := range m.pr.runs {
		lines = append(lines, prLine{text: m.checkRow(run), ref: i, sub: -1, src: sourceRun, file: -1})
	}
	return lines
}

func (m model) checkRow(check gh.Check) string {
	glyph, word, style := checkLook(check.State)
	name := sanitize(check.Name)
	if check.Workflow != "" {
		name += faintStyle.Render(" · " + sanitize(check.Workflow))
	}
	return style.Render(glyph+" "+fmt.Sprintf("%-8s", word)) + " " + name + faintStyle.Render(m.checkDuration(check))
}

func checkLook(state gh.CheckState) (string, string, lipgloss.Style) {
	switch state {
	case gh.CheckPassing:
		return "✓", "passed", runningStyle
	case gh.CheckFailing:
		return "✗", "failed", failedStyle
	case gh.CheckSkipped:
		return "–", "skipped", faintStyle
	}
	return "●", "pending", waitingStyle
}

func (m model) checkDuration(check gh.Check) string {
	if check.StartedAt.IsZero() {
		return ""
	}
	end := check.CompletedAt
	if end.IsZero() {
		end = m.now
	}
	if end.IsZero() || end.Before(check.StartedAt) {
		return ""
	}
	return " · " + end.Sub(check.StartedAt).Round(1e9).String()
}

func (m model) commentLines(pr gh.PR, width int) []prLine {
	var lines []prLine
	for i, comment := range pr.Comments {
		heading := sanitize(comment.Author.Login) + " commented" + m.agoText(comment.CreatedAt)
		lines = append(lines, bodyLines(heading, comment.Body, width, i, -1, sourceComment)...)
	}
	for _, review := range pr.Reviews {
		if strings.TrimSpace(review.Body) == "" && review.State == "COMMENTED" {
			continue
		}
		heading := sanitize(review.Author.Login) + " reviewed: " + strings.ToLower(strings.ReplaceAll(review.State, "_", " "))
		lines = append(lines, bodyLines(heading, review.Body, width, -1, -1, sourceNone)...)
	}
	lines = append(lines, m.threadLines(pr, width)...)
	if len(lines) == 0 {
		return plainLines(faintStyle.Render("No comments yet"))
	}
	return lines
}

func (m model) agoText(at time.Time) string {
	if m.now.IsZero() || at.IsZero() {
		return ""
	}
	return " " + ageAt(at, m.now) + " ago"
}

func bodyLines(heading, body string, width, ref, sub int, src prSource) []prLine {
	lines := []prLine{{text: titleStyle.Render(heading), ref: ref, sub: sub, src: src, file: -1}}
	for _, text := range strings.Split(strings.TrimSpace(sanitizeBody(body)), "\n") {
		for _, wrapped := range strings.Split(ansi.Wrap(text, max(1, width-2), ""), "\n") {
			lines = append(lines, prLine{text: "  " + wrapped, ref: ref, sub: sub, src: src, file: -1})
		}
	}
	return append(lines, prLine{ref: -1, sub: -1, file: -1})
}

func (m model) threadLines(pr gh.PR, width int) []prLine {
	data, loaded := m.pr.threads[pr.Number]
	if !loaded {
		return plainLines(faintStyle.Render("loading review threads…"))
	}
	var lines []prLine
	if data.threads.Total > len(data.threads.Threads) {
		lines = append(lines, plainLines(faintStyle.Render(fmt.Sprintf("showing %d of %d threads", len(data.threads.Threads), data.threads.Total)))...)
	}
	for i, thread := range data.threads.Threads {
		lines = append(lines, threadBlock(thread, width, i)...)
	}
	return lines
}

func threadBlock(thread gh.Thread, width, index int) []prLine {
	state := failedStyle.Render("unresolved")
	if thread.Resolved {
		state = runningStyle.Render("resolved")
	}
	where := sanitize(thread.Path)
	if thread.Line > 0 {
		where += ":" + strconv.Itoa(thread.Line)
	}
	header := titleStyle.Render(where) + " · " + state
	if thread.Outdated {
		header += faintStyle.Render(" · outdated")
	}
	lines := []prLine{{text: header, ref: index, sub: -1, src: sourceThread, file: -1}}
	for sub, comment := range thread.Comments {
		lines = append(lines, bodyLines(sanitize(comment.Author.Login), comment.Body, width, index, sub, sourceThread)...)
	}
	return lines
}

func (m model) diffLines(pr gh.PR) []prLine {
	data, loaded := m.pr.diffs[pr.Number]
	switch {
	case !loaded:
		return plainLines(faintStyle.Render(loadingText))
	case data.tooLarge:
		return plainLines(failedStyle.Render("diff too large, open on GitHub (o)"))
	case len(data.files) == 0:
		return plainLines(faintStyle.Render("No changes"))
	}
	var lines []prLine
	for i, file := range data.files {
		lines = append(lines, prLine{text: fileHeader(file), ref: -1, sub: -1, kind: fileHeaderLine, file: i})
		lines = append(lines, diffBody(file, i, m.pendingLines(pr))...)
	}
	return lines
}

func fileHeader(file gh.FileDiff) string {
	name := sanitize(file.Path)
	if file.Renamed {
		name = sanitize(file.OldPath) + " → " + name
	}
	return titleStyle.Render(name) + " " + countsText(file.Added, file.Removed, 0)
}

func diffBody(file gh.FileDiff, index int, pending map[string]bool) []prLine {
	lines := make([]prLine, 0, len(file.Lines))
	for _, line := range file.Lines {
		text := sanitize(line.Text)
		gutter := " "
		if line.NewLine > 0 && pending[fmt.Sprintf("%s:%d", file.Path, line.NewLine)] {
			gutter = pendingMark
		}
		lines = append(lines, prLine{text: gutter + diffLineStyle(text).Render(text), ref: -1, sub: -1, kind: diffBodyLine, file: index, newLine: line.NewLine})
	}
	if len(lines) == 0 {
		lines = append(lines, prLine{text: faintStyle.Render("no textual changes"), ref: -1, sub: -1, file: index})
	}
	return lines
}

func (m *model) stepFile(delta int) {
	p := &m.pr
	lines := m.prTabLines()
	cursor := &p.tabCursor[p.tab]
	if p.tab != diffTab {
		return
	}
	for i := *cursor + delta; i >= 0 && i < len(lines); i += delta {
		if lines[i].kind == fileHeaderLine {
			*cursor = i
			return
		}
	}
}

func (m model) prSessions(pr gh.PR) []trail.Session {
	if pr.CrossRepo || pr.HeadRef == "" {
		return nil
	}
	var matched []trail.Session
	for key, session := range m.sessions {
		if m.isChild(session) || session.Events == 0 {
			continue
		}
		if slices.Contains(session.Branches, pr.HeadRef) || (len(session.Branches) == 0 && session.Branch == pr.HeadRef) {
			matched = append(matched, m.sessions[key])
		}
	}
	slices.SortFunc(matched, func(a, b trail.Session) int { return b.Last.Compare(a.Last) })
	return matched
}

func (m model) sessionSummary(pr gh.PR) string {
	matched := m.prSessions(pr)
	if len(matched) == 0 {
		return faintStyle.Render("none linked")
	}
	total := m.familySession(matched[0].Key)
	for _, session := range matched[1:] {
		total = total.WithUsageOf(m.familySession(session.Key))
	}
	running := ""
	if slices.ContainsFunc(matched, func(s trail.Session) bool { return s.Liveness == trail.LivenessRunning }) {
		running = " · " + runningStyle.Render("running")
	}
	return fmt.Sprintf("%d · %s%s", len(matched), strings.TrimSpace(ansi.Strip(sessionCost(total))), running)
}

func (m model) sessionLines(pr gh.PR) []prLine {
	matched := m.prSessions(pr)
	if len(matched) == 0 {
		return plainLines(faintStyle.Render(m.noSessionsText(pr)))
	}
	lines := make([]prLine, len(matched))
	for i, session := range matched {
		row := glyphStyle(session).Render(stateGlyph(session)) + cliStyle(session.CLI).Render(m.cliIcon(session.CLI)) + sanitize(sessionLabel(session))
		lines[i] = prLine{text: spread(row, faintStyle.Render(ageAt(session.Last, m.now))+" "+costCell(m.familySession(session.Key)), m.prDetailWidth()), ref: i, sub: -1, file: -1}
	}
	return lines
}

func (m model) noSessionsText(pr gh.PR) string {
	switch {
	case pr.CrossRepo:
		return "Pull requests from a fork are not linked to sessions"
	case pr.HeadRef == "":
		return "This pull request has no branch to match"
	}
	return "No session has worked on " + sanitize(pr.HeadRef)
}

func (m *model) activateLine() tea.Cmd {
	p := &m.pr
	pr, ok := p.selected()
	line, found := m.cursorLine()
	if !ok || !found || p.tab != sessionsTab {
		return nil
	}
	matched := m.prSessions(pr)
	if line.ref < 0 || line.ref >= len(matched) {
		return nil
	}
	return m.jumpToSessionFromPR(matched[line.ref].Key)
}

func (m *model) jumpToSessionFromPR(key string) tea.Cmd {
	m.revealSession(key)
	row := m.rowOf(key)
	if row == allSessionsRow {
		m.notice = "that session is hidden by a filter; press m or o in the main view to show it"
		return nil
	}
	m.closePRs()
	m.selectSession(row)
	m.focus = eventsPane
	return nil
}
