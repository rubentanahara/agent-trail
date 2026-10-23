package gh

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"
)

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100){totalCount nodes{id isResolved isOutdated path line comments(first:50){nodes{id body state createdAt url viewerCanUpdate author{login}}}}} reviews(first:10,states:PENDING){nodes{id commit{oid} comments{totalCount}}}}}}`

const (
	replyMutation      = `mutation($threadId:ID!,$body:String!){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$threadId,body:$body}){comment{id}}}`
	resolveMutation    = `mutation($threadId:ID!){resolveReviewThread(input:{threadId:$threadId}){thread{id}}}`
	unresolveMutation  = `mutation($threadId:ID!){unresolveReviewThread(input:{threadId:$threadId}){thread{id}}}`
	editIssueMutation  = `mutation($id:ID!,$body:String!){updateIssueComment(input:{id:$id,body:$body}){issueComment{id}}}`
	editReviewMutation = `mutation($id:ID!,$body:String!){updatePullRequestReviewComment(input:{pullRequestReviewCommentId:$id,body:$body}){pullRequestReviewComment{id}}}`
	startMutation      = `mutation($prId:ID!,$commit:GitObjectID!){addPullRequestReview(input:{pullRequestId:$prId,commitOID:$commit}){pullRequestReview{id}}}`
	lineThreadMutation = `mutation($reviewId:ID!,$path:String!,$line:Int!,$body:String!){addPullRequestReviewThread(input:{pullRequestReviewId:$reviewId,path:$path,line:$line,side:RIGHT,body:$body}){thread{id}}}`
	submitMutation     = `mutation($reviewId:ID!,$event:PullRequestReviewEvent!,$body:String!){submitPullRequestReview(input:{pullRequestReviewId:$reviewId,event:$event,body:$body}){pullRequestReview{id}}}`
	discardMutation    = `mutation($reviewId:ID!){deletePullRequestReview(input:{pullRequestReviewId:$reviewId}){pullRequestReview{id}}}`
)

type SubmitEvent string

const (
	SubmitApprove        SubmitEvent = "APPROVE"
	SubmitRequestChanges SubmitEvent = "REQUEST_CHANGES"
	SubmitComment        SubmitEvent = "COMMENT"
)

func graphqlArgs(query string, strings map[string]string, files map[string]string, ints map[string]int) []string {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, name := range sortedKeys(strings) {
		args = append(args, "-f", name+"="+strings[name])
	}
	for _, name := range sortedKeys(ints) {
		args = append(args, "-F", name+"="+strconv.Itoa(ints[name]))
	}
	for _, name := range sortedKeys(files) {
		args = append(args, "-F", name+"=@"+files[name])
	}
	return args
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func ReplyArgs(threadID, bodyFile string) []string {
	return graphqlArgs(replyMutation, map[string]string{"threadId": threadID}, map[string]string{"body": bodyFile}, nil)
}

func ResolveArgs(threadID string) []string {
	return graphqlArgs(resolveMutation, map[string]string{"threadId": threadID}, nil, nil)
}

func UnresolveArgs(threadID string) []string {
	return graphqlArgs(unresolveMutation, map[string]string{"threadId": threadID}, nil, nil)
}

func EditIssueCommentArgs(commentID, bodyFile string) []string {
	return graphqlArgs(editIssueMutation, map[string]string{"id": commentID}, map[string]string{"body": bodyFile}, nil)
}

func EditReviewCommentArgs(commentID, bodyFile string) []string {
	return graphqlArgs(editReviewMutation, map[string]string{"id": commentID}, map[string]string{"body": bodyFile}, nil)
}

func StartReviewArgs(pullRequestID, commitOID string) []string {
	return graphqlArgs(startMutation, map[string]string{"prId": pullRequestID, "commit": commitOID}, nil, nil)
}

func LineThreadArgs(reviewID, path string, line int, bodyFile string) []string {
	return graphqlArgs(lineThreadMutation, map[string]string{"reviewId": reviewID, "path": path}, map[string]string{"body": bodyFile}, map[string]int{"line": line})
}

func SubmitReviewArgs(reviewID string, event SubmitEvent, bodyFile string) []string {
	return graphqlArgs(submitMutation, map[string]string{"reviewId": reviewID, "event": string(event)}, map[string]string{"body": bodyFile}, nil)
}

func DiscardReviewArgs(reviewID string) []string {
	return graphqlArgs(discardMutation, map[string]string{"reviewId": reviewID}, nil, nil)
}

func DecodeStartedReview(data []byte) (string, error) {
	var raw struct {
		Data struct {
			AddPullRequestReview struct {
				PullRequestReview struct {
					ID string `json:"id"`
				} `json:"pullRequestReview"`
			} `json:"addPullRequestReview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	return raw.Data.AddPullRequestReview.PullRequestReview.ID, nil
}

type ThreadComment struct {
	Pending   bool
	ID        string
	Author    Author
	Body      string
	CreatedAt time.Time
	URL       string
	CanUpdate bool
}

type Thread struct {
	ID       string
	Resolved bool
	Outdated bool
	Path     string
	Line     int
	Comments []ThreadComment
}

type PendingReview struct {
	ID        string
	CommitOID string
	Comments  int
}

type Threads struct {
	Threads []Thread
	Pending *PendingReview
	Total   int
}

func ThreadsArgs(owner, name string, number int) []string {
	return []string{
		"api", "graphql",
		"-f", "query=" + threadsQuery,
		"-f", "owner=" + owner,
		"-f", "name=" + name,
		"-F", "number=" + strconv.Itoa(number),
	}
}

type rawThreadComment struct {
	State     string     `json:"state"`
	ID        string     `json:"id"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"createdAt"`
	URL       string     `json:"url"`
	CanUpdate bool       `json:"viewerCanUpdate"`
	Author    *rawAuthor `json:"author"`
}

type rawThread struct {
	ID       string `json:"id"`
	Resolved bool   `json:"isResolved"`
	Outdated bool   `json:"isOutdated"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Comments struct {
		Nodes []rawThreadComment `json:"nodes"`
	} `json:"comments"`
}

type rawPending struct {
	ID     string `json:"id"`
	Commit struct {
		OID string `json:"oid"`
	} `json:"commit"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
}

type rawThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					TotalCount int         `json:"totalCount"`
					Nodes      []rawThread `json:"nodes"`
				} `json:"reviewThreads"`
				Reviews struct {
					Nodes []rawPending `json:"nodes"`
				} `json:"reviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func DecodeThreads(data []byte) (Threads, error) {
	var raw rawThreadsResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return Threads{}, err
	}
	source := raw.Data.Repository.PullRequest
	threads := Threads{Total: source.ReviewThreads.TotalCount}
	for _, node := range source.ReviewThreads.Nodes {
		thread := Thread{ID: node.ID, Resolved: node.Resolved, Outdated: node.Outdated, Path: node.Path, Line: node.Line}
		for _, comment := range node.Comments.Nodes {
			thread.Comments = append(thread.Comments, ThreadComment{
				Pending: comment.State == "PENDING", ID: comment.ID, Author: comment.Author.decode(), Body: comment.Body,
				CreatedAt: comment.CreatedAt, URL: comment.URL, CanUpdate: comment.CanUpdate,
			})
		}
		threads.Threads = append(threads.Threads, thread)
	}
	if len(source.Reviews.Nodes) > 0 {
		pending := source.Reviews.Nodes[0]
		threads.Pending = &PendingReview{ID: pending.ID, CommitOID: pending.Commit.OID, Comments: pending.Comments.TotalCount}
	}
	return threads, nil
}

var (
	ErrPendingOnOlderCommit = errors.New("your pending review was started on an older commit; submit or discard it first")
	ErrNoReviewStarted      = errors.New("GitHub did not return the new review")
)

func AddLineComment(ctx context.Context, run Runner, repo Repo, number int, pullRequestID, head, path string, line int, bodyFile string) error {
	reviewID, err := pendingReviewFor(ctx, run, repo, number, pullRequestID, head)
	if err != nil {
		return err
	}
	_, err = run(ctx, LineThreadArgs(reviewID, path, line, bodyFile))
	return err
}

func pendingReviewFor(ctx context.Context, run Runner, repo Repo, number int, pullRequestID, head string) (string, error) {
	out, err := run(ctx, ThreadsArgs(repo.Owner, repo.Name, number))
	if err != nil {
		return "", err
	}
	threads, err := DecodeThreads(out)
	if err != nil {
		return "", err
	}
	if threads.Pending != nil {
		if threads.Pending.CommitOID != head {
			return "", ErrPendingOnOlderCommit
		}
		return threads.Pending.ID, nil
	}
	return startReview(ctx, run, pullRequestID, head)
}

func startReview(ctx context.Context, run Runner, pullRequestID, head string) (string, error) {
	out, err := run(ctx, StartReviewArgs(pullRequestID, head))
	if err != nil {
		return "", err
	}
	id, err := DecodeStartedReview(out)
	if err == nil && id == "" {
		return "", ErrNoReviewStarted
	}
	return id, err
}
