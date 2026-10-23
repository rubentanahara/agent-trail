package gh

import (
	"strconv"
)

type MergeMethod string

const (
	MergeSquash MergeMethod = "--squash"
	MergeCommit MergeMethod = "--merge"
	MergeRebase MergeMethod = "--rebase"
)

type ReviewEvent string

const (
	ReviewApprove        ReviewEvent = "--approve"
	ReviewRequestChanges ReviewEvent = "--request-changes"
	ReviewComment        ReviewEvent = "--comment"
)

func (r Repo) Methods() []MergeMethod {
	var methods []MergeMethod
	if r.AllowSquash {
		methods = append(methods, MergeSquash)
	}
	if r.AllowMerge {
		methods = append(methods, MergeCommit)
	}
	if r.AllowRebase {
		methods = append(methods, MergeRebase)
	}
	return methods
}

func CommentArgs(number int, bodyFile string) []string {
	return []string{"pr", "comment", strconv.Itoa(number), "--body-file=" + bodyFile}
}

func EditArgs(number int, title, bodyFile string) []string {
	return []string{"pr", "edit", strconv.Itoa(number), "--title=" + title, "--body-file=" + bodyFile}
}

func CloseArgs(number int) []string {
	return []string{"pr", "close", strconv.Itoa(number)}
}

func CheckoutArgs(number int) []string {
	return []string{"pr", "checkout", strconv.Itoa(number)}
}

func MergeArgs(repo string, number int, method MergeMethod, deleteBranch, auto bool) []string {
	args := []string{"pr", "merge", strconv.Itoa(number), "--repo=" + repo, string(method)}
	if deleteBranch {
		args = append(args, "--delete-branch")
	}
	if auto {
		args = append(args, "--auto")
	}
	return args
}

func ReviewArgs(number int, event ReviewEvent, bodyFile string) []string {
	return []string{"pr", "review", strconv.Itoa(number), string(event), "--body-file=" + bodyFile}
}

func RerunArgs(runID string, failedOnly bool) []string {
	args := []string{"run", "rerun", runID}
	if failedOnly {
		args = append(args, "--failed")
	}
	return args
}
