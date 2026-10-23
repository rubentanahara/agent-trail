package gh

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	RunLimit     = 15
	runFetchSize = 50
	runFields    = "databaseId,displayTitle,workflowName,status,conclusion,event,headBranch,url,startedAt,updatedAt"
)

var pullRequestEvents = []string{"pull_request", "pull_request_target"}

type rawRun struct {
	Title      string    `json:"displayTitle"`
	Workflow   string    `json:"workflowName"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	Event      string    `json:"event"`
	Branch     string    `json:"headBranch"`
	URL        string    `json:"url"`
	StartedAt  time.Time `json:"startedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func RunsArgs() []string {
	return []string{"run", "list", "--limit=" + strconv.Itoa(runFetchSize), "--json=" + runFields}
}

func Runs(ctx context.Context, run Runner) ([]Check, error) {
	out, err := run(ctx, RunsArgs())
	if err != nil {
		return nil, err
	}
	return DecodeRuns(out)
}

func DecodeRuns(data []byte) ([]Check, error) {
	var raw []rawRun
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var runs []Check
	for _, r := range raw {
		if isPullRequestEvent(r.Event) {
			continue
		}
		runs = append(runs, r.decode())
		if len(runs) == RunLimit {
			break
		}
	}
	return runs, nil
}

func isPullRequestEvent(event string) bool {
	for _, name := range pullRequestEvents {
		if event == name {
			return true
		}
	}
	return false
}

func (r rawRun) decode() Check {
	check := Check{
		Name:      r.Title,
		Workflow:  strings.Join([]string{r.Workflow, r.Event, r.Branch}, " · "),
		State:     runState(strings.ToUpper(r.Status), strings.ToUpper(r.Conclusion)),
		URL:       r.URL,
		StartedAt: r.StartedAt,
	}
	if check.State != CheckPending {
		check.CompletedAt = r.UpdatedAt
	}
	return check
}
