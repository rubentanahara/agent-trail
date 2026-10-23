package gh

import (
	"errors"
	"os/exec"
	"strings"
)

type Problem int

const (
	Missing Problem = iota
	Unauthenticated
	NoRemote
	AmbiguousRemote
	MissingScope
	OutdatedGh
	RateLimited
	Other
)

type stderrSignature struct {
	problem Problem
	needles []string
}

var stderrSignatures = []stderrSignature{
	{RateLimited, []string{"rate limit", "http 429"}},
	{NoRemote, []string{"no git remotes found", "none of the git remotes"}},
	{Unauthenticated, []string{"gh auth login", "not logged in", "authentication required"}},
	{AmbiguousRemote, []string{"no default remote repository", "gh repo set-default"}},
	{MissingScope, []string{"required scope"}},
	{OutdatedGh, []string{"unknown flag", "unknown command"}},
}

var remedies = map[Problem]string{
	Missing:         "gh is not installed. Install it from https://cli.github.com and reopen this screen.",
	Unauthenticated: "gh is not signed in. Run `gh auth login`, then press R.",
	NoRemote:        "This directory has no GitHub remote. Add one or open a GitHub repository.",
	AmbiguousRemote: "Several remotes match. Run `gh repo set-default`, then press R.",
	MissingScope:    "The gh token lacks a scope. Run `gh auth refresh -s repo`, then press R.",
	OutdatedGh:      "This gh is too old for the screen. Upgrade it with your package manager.",
	RateLimited:     "GitHub rate limit reached. Polling pauses; press R to try again later.",
	Other:           "gh failed. Press R to retry.",
}

func (p Problem) Remedy() string { return remedies[p] }

func ProblemOf(err error) Problem {
	if errors.Is(err, exec.ErrNotFound) {
		return Missing
	}
	var runErr *Error
	if !errors.As(err, &runErr) {
		return Other
	}
	stderr := strings.ToLower(runErr.Stderr)
	for _, signature := range stderrSignatures {
		if containsAny(stderr, signature.needles) {
			return signature.problem
		}
	}
	return Other
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
