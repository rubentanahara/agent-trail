package gh

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ListLimit      = 100
	listFields     = "number,title,author,headRefName,baseRefName,isDraft,isCrossRepository,updatedAt,headRefOid,reviewDecision,mergeable,mergeStateStatus,statusCheckRollup,url,reviewRequests,state"
	viewFields     = listFields + ",id,body,comments,reviews,files,additions,deletions,labels"
	viewerCacheTTL = "1h"
	checkRunKind   = "CheckRun"
	botSuffix      = "[bot]"
	botPrefix      = "app/"
)

type CheckState int

const (
	CheckPending CheckState = iota
	CheckPassing
	CheckFailing
	CheckSkipped
)

type Author struct {
	Login string
	Bot   bool
}

type Check struct {
	Name        string
	Workflow    string
	State       CheckState
	URL         string
	StartedAt   time.Time
	CompletedAt time.Time
	StatusOnly  bool
}

type File struct {
	Path      string
	Additions int
	Deletions int
	Change    string
}

type Review struct {
	Author      Author
	State       string
	Body        string
	SubmittedAt time.Time
}

type Comment struct {
	CanEdit   bool
	ID        string
	Author    Author
	Body      string
	CreatedAt time.Time
	URL       string
}

type PR struct {
	ID             string
	Number         int
	Title          string
	Body           string
	Author         Author
	HeadRef        string
	BaseRef        string
	HeadSHA        string
	State          string
	Draft          bool
	CrossRepo      bool
	UpdatedAt      time.Time
	ReviewDecision string
	Mergeable      string
	MergeState     string
	URL            string
	Checks         []Check
	ReviewRequests []string
	Reviews        []Review
	Comments       []Comment
	Files          []File
	Additions      int
	Deletions      int
	Labels         []string
}

type Sections struct {
	NeedsReview []PR
	Mine        []PR
	All         []PR
}

func ListArgs() []string {
	return []string{"pr", "list", "--state=open", "--limit=" + strconv.Itoa(ListLimit), "--json=" + listFields}
}

func ReviewRequestedArgs() []string {
	return []string{"pr", "list", "--state=open", "--limit=" + strconv.Itoa(ListLimit), "--search=review-requested:@me", "--json=" + listFields}
}

func ViewArgs(number int) []string {
	return []string{"pr", "view", strconv.Itoa(number), "--json=" + viewFields}
}

func ViewerArgs() []string {
	return []string{"api", "user", "--cache=" + viewerCacheTTL, "--jq=.login"}
}

func List(ctx context.Context, run Runner) ([]PR, error) {
	out, err := run(ctx, ListArgs())
	if err != nil {
		return nil, err
	}
	return DecodeList(out)
}

func ListReviewRequested(ctx context.Context, run Runner) ([]PR, error) {
	out, err := run(ctx, ReviewRequestedArgs())
	if err != nil {
		return nil, err
	}
	return DecodeList(out)
}

func View(ctx context.Context, run Runner, number int) (PR, error) {
	out, err := run(ctx, ViewArgs(number))
	if err != nil {
		return PR{}, err
	}
	return DecodeView(out)
}

func Viewer(ctx context.Context, run Runner) (string, error) {
	out, err := run(ctx, ViewerArgs())
	return strings.TrimSpace(string(out)), err
}

func Partition(open, requested []PR, viewer string) Sections {
	asked := map[int]bool{}
	for _, pr := range requested {
		asked[pr.Number] = true
	}
	sections := Sections{All: open}
	for _, pr := range open {
		if asked[pr.Number] {
			sections.NeedsReview = append(sections.NeedsReview, pr)
		}
		if viewer != "" && pr.Author.Login == viewer {
			sections.Mine = append(sections.Mine, pr)
		}
	}
	return sections
}

func (p PR) Pending() bool {
	return slices.ContainsFunc(p.Checks, func(c Check) bool { return c.State == CheckPending })
}

func (p PR) Failing() bool {
	return slices.ContainsFunc(p.Checks, func(c Check) bool { return c.State == CheckFailing })
}

type rawAuthor struct {
	Login string `json:"login"`
	IsBot bool   `json:"is_bot"`
}

type rawCheck struct {
	Kind        string    `json:"__typename"`
	Name        string    `json:"name"`
	Context     string    `json:"context"`
	Workflow    string    `json:"workflowName"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	State       string    `json:"state"`
	DetailsURL  string    `json:"detailsUrl"`
	TargetURL   string    `json:"targetUrl"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

type rawReview struct {
	Author      *rawAuthor `json:"author"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	SubmittedAt time.Time  `json:"submittedAt"`
}

type rawComment struct {
	ViewerDidAuthor bool       `json:"viewerDidAuthor"`
	ID              string     `json:"id"`
	Author          *rawAuthor `json:"author"`
	Body            string     `json:"body"`
	CreatedAt       time.Time  `json:"createdAt"`
	URL             string     `json:"url"`
}

type rawFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Change    string `json:"changeType"`
}

type rawRequest struct {
	Login string `json:"login"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
}

type rawLabel struct {
	Name string `json:"name"`
}

type rawPR struct {
	ID             string       `json:"id"`
	Number         int          `json:"number"`
	Title          string       `json:"title"`
	Body           string       `json:"body"`
	Author         *rawAuthor   `json:"author"`
	HeadRef        string       `json:"headRefName"`
	BaseRef        string       `json:"baseRefName"`
	HeadSHA        string       `json:"headRefOid"`
	State          string       `json:"state"`
	Draft          bool         `json:"isDraft"`
	CrossRepo      bool         `json:"isCrossRepository"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	ReviewDecision string       `json:"reviewDecision"`
	Mergeable      string       `json:"mergeable"`
	MergeState     string       `json:"mergeStateStatus"`
	URL            string       `json:"url"`
	Rollup         []rawCheck   `json:"statusCheckRollup"`
	Requests       []rawRequest `json:"reviewRequests"`
	Reviews        []rawReview  `json:"reviews"`
	Comments       []rawComment `json:"comments"`
	Files          []rawFile    `json:"files"`
	Additions      int          `json:"additions"`
	Deletions      int          `json:"deletions"`
	Labels         []rawLabel   `json:"labels"`
}

func DecodeList(data []byte) ([]PR, error) {
	var raw []rawPR
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	prs := make([]PR, len(raw))
	for i, item := range raw {
		prs[i] = item.pr()
	}
	return prs, nil
}

func DecodeView(data []byte) (PR, error) {
	var raw rawPR
	if err := json.Unmarshal(data, &raw); err != nil {
		return PR{}, err
	}
	return raw.pr(), nil
}

func (r rawPR) pr() PR {
	pr := PR{
		ID: r.ID, Number: r.Number, Title: r.Title, Body: r.Body, Author: r.Author.decode(),
		HeadRef: r.HeadRef, BaseRef: r.BaseRef, HeadSHA: r.HeadSHA, State: r.State,
		Draft: r.Draft, CrossRepo: r.CrossRepo, UpdatedAt: r.UpdatedAt,
		ReviewDecision: r.ReviewDecision, Mergeable: r.Mergeable, MergeState: r.MergeState,
		URL: r.URL, Additions: r.Additions, Deletions: r.Deletions,
	}
	for _, check := range r.Rollup {
		pr.Checks = append(pr.Checks, check.decode())
	}
	for _, request := range r.Requests {
		pr.ReviewRequests = append(pr.ReviewRequests, firstNonEmpty(request.Login, request.Slug, request.Name))
	}
	for _, review := range r.Reviews {
		pr.Reviews = append(pr.Reviews, Review{Author: review.Author.decode(), State: review.State, Body: review.Body, SubmittedAt: review.SubmittedAt})
	}
	for _, comment := range r.Comments {
		pr.Comments = append(pr.Comments, Comment{CanEdit: comment.ViewerDidAuthor, ID: comment.ID, Author: comment.Author.decode(), Body: comment.Body, CreatedAt: comment.CreatedAt, URL: comment.URL})
	}
	for _, file := range r.Files {
		pr.Files = append(pr.Files, File(file))
	}
	for _, label := range r.Labels {
		pr.Labels = append(pr.Labels, label.Name)
	}
	return pr
}

func (a *rawAuthor) decode() Author {
	if a == nil {
		return Author{Login: "ghost"}
	}
	login := strings.TrimPrefix(a.Login, botPrefix)
	return Author{Login: login, Bot: a.IsBot || strings.HasSuffix(login, botSuffix)}
}

func (c rawCheck) decode() Check {
	if c.Kind == checkRunKind {
		return Check{
			Name: c.Name, Workflow: c.Workflow, State: runState(c.Status, c.Conclusion),
			URL: c.DetailsURL, StartedAt: c.StartedAt, CompletedAt: realTime(c.CompletedAt),
		}
	}
	return Check{
		Name: c.Context, State: contextState(c.State), URL: c.TargetURL,
		StartedAt: c.StartedAt, StatusOnly: true,
	}
}

func runState(status, conclusion string) CheckState {
	if status != "COMPLETED" {
		return CheckPending
	}
	switch conclusion {
	case "SUCCESS":
		return CheckPassing
	case "NEUTRAL", "SKIPPED":
		return CheckSkipped
	}
	return CheckFailing
}

func contextState(state string) CheckState {
	switch state {
	case "SUCCESS":
		return CheckPassing
	case "PENDING", "EXPECTED":
		return CheckPending
	}
	return CheckFailing
}

func realTime(t time.Time) time.Time {
	if t.Year() <= 1 {
		return time.Time{}
	}
	return t
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func RateResetArgs() []string {
	return []string{"api", "rate_limit", "--jq=.resources.graphql.reset"}
}

func ResetTime(ctx context.Context, run Runner) (time.Time, bool) {
	out, err := run(ctx, RateResetArgs())
	if err != nil {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0), true
}
