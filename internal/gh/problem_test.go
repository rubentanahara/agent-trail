package gh

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestProblemOfClassifiesCapturedStderr(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   Problem
	}{
		{"not logged in", "To get started with GitHub CLI, please run:  gh auth login\n", Unauthenticated},
		{"no token", "You are not logged into any GitHub hosts. To log in, run: gh auth login\n", Unauthenticated},
		{"no remote", "no git remotes found\n", NoRemote},
		{"non github remote", "none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login`\n", NoRemote},
		{"ambiguous", "X No default remote repository has been set. To learn more about the default repository, run: gh repo set-default --help\n", AmbiguousRemote},
		{"scope", "GraphQL: Resource not accessible by personal access token (HTTP 403) - your token has not been granted the required scopes\n", MissingScope},
		{"old gh", "unknown flag: --json\n", OutdatedGh},
		{"rate limit", "GraphQL: API rate limit already exceeded for user ID 1. (HTTP 403)\n", RateLimited},
		{"too many", "HTTP 429: Too Many Requests\n", RateLimited},
		{"other", "HTTP 502: Bad Gateway\n", Other},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := &Error{Args: []string{"pr", "list"}, Stderr: c.stderr, Err: errors.New("exit status 1")}

			if got := ProblemOf(err); got != c.want {
				t.Fatalf("ProblemOf = %v, want %v", got, c.want)
			}
		})
	}
}

func TestProblemOfSeesThroughWrapping(t *testing.T) {
	err := fmt.Errorf("list: %w", &Error{Err: exec.ErrNotFound})

	if got := ProblemOf(err); got != Missing {
		t.Fatalf("ProblemOf = %v, want Missing", got)
	}
}

func TestProblemOfForeignErrorIsOther(t *testing.T) {
	if got := ProblemOf(errors.New("plain")); got != Other {
		t.Fatalf("ProblemOf = %v, want Other", got)
	}
}

func TestProblemRemedyIsNeverEmpty(t *testing.T) {
	for p := Missing; p <= Other; p++ {
		if p.Remedy() == "" {
			t.Fatalf("Problem %d has no remedy", p)
		}
	}
}
