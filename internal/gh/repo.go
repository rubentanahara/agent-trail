package gh

import (
	"context"
	"encoding/json"
	"strings"
)

type Repo struct {
	FullName       string
	Owner          string
	Name           string
	AllowSquash    bool
	AllowMerge     bool
	AllowRebase    bool
	AllowAutoMerge bool
	DeleteOnMerge  bool
}

func RepoArgs() []string {
	return []string{"api", "repos/{owner}/{repo}", "--cache=" + viewerCacheTTL}
}

func LoadRepo(ctx context.Context, run Runner) (Repo, error) {
	out, err := run(ctx, RepoArgs())
	if err != nil {
		return Repo{}, err
	}
	return DecodeRepo(out)
}

func DecodeRepo(data []byte) (Repo, error) {
	var raw struct {
		FullName       string `json:"full_name"`
		AllowSquash    bool   `json:"allow_squash_merge"`
		AllowMerge     bool   `json:"allow_merge_commit"`
		AllowRebase    bool   `json:"allow_rebase_merge"`
		AllowAutoMerge bool   `json:"allow_auto_merge"`
		DeleteOnMerge  bool   `json:"delete_branch_on_merge"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Repo{}, err
	}
	owner, name, _ := strings.Cut(raw.FullName, "/")
	return Repo{
		FullName: raw.FullName, Owner: owner, Name: name,
		AllowSquash: raw.AllowSquash, AllowMerge: raw.AllowMerge, AllowRebase: raw.AllowRebase,
		AllowAutoMerge: raw.AllowAutoMerge, DeleteOnMerge: raw.DeleteOnMerge,
	}, nil
}
