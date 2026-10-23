package trail

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	gitEntry         = ".git"
	gitFilePrefix    = "gitdir:"
	gitHeadFile      = "HEAD"
	gitCommonDirFile = "commondir"
	gitWorktreesDir  = "worktrees"
	gitDirPointer    = "gitdir"
	gitBranchRef     = "ref: refs/heads/"
)

type checkouts struct {
	repo      string
	commonDir string
	stamp     time.Time
	roots     []string
}

func newCheckouts(repo string) *checkouts {
	c := &checkouts{repo: repo, roots: []string{repo}}
	if repo != EveryRepository {
		c.commonDir = commonDirOf(repo)
		c.load()
	}
	return c
}

func (c *checkouts) refresh() bool {
	if c.commonDir == "" || c.worktreesStamp().Equal(c.stamp) {
		return false
	}
	before := c.roots
	c.load()
	return !slices.Equal(before, c.roots)
}

func (c *checkouts) worktreesStamp() time.Time {
	info, err := os.Stat(filepath.Join(c.commonDir, gitWorktreesDir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (c *checkouts) load() {
	c.stamp = c.worktreesStamp()
	roots := []string{c.repo}
	if filepath.Base(c.commonDir) == gitEntry {
		roots = append(roots, filepath.Dir(c.commonDir))
	}
	pointers, _ := filepath.Glob(filepath.Join(c.commonDir, gitWorktreesDir, "*", gitDirPointer))
	for _, pointer := range pointers {
		if gitFile := firstLine(pointer); gitFile != "" {
			roots = append(roots, filepath.Dir(gitFile))
		}
	}
	slices.Sort(roots[1:])
	c.roots = slices.Compact(roots)
}

func (t *Trail) refreshCheckouts() {
	if t.checkouts.refresh() {
		t.rejected = map[string]bool{}
		t.undecided = map[string]int64{}
	}
}

func (t *Trail) belongsToAny(path string, src *source) (belongs, decided bool) {
	for _, root := range t.checkouts.roots {
		belongs, decided = src.belongs(path, root)
		if belongs || !decided {
			return belongs, decided
		}
	}
	return false, true
}

func (t *Trail) claudeCandidates(home string) []string {
	var files []string
	for _, root := range t.checkouts.roots {
		files = append(files, claudeCandidates(home, root)...)
	}
	slices.Sort(files)
	return slices.Compact(files)
}

func (t *Trail) applyCheckoutBranches() {
	known := map[string]string{}
	for key := range t.changed {
		session := t.sessions[key]
		if session.Cwd == "" {
			continue
		}
		branch, seen := known[session.Cwd]
		if !seen {
			branch = checkoutBranch(session.Cwd)
			known[session.Cwd] = branch
		}
		if branch != "" {
			session.Branch = branch
		}
	}
}

func checkoutBranch(cwd string) string {
	root := checkoutRoot(cwd)
	if root == "" {
		return ""
	}
	head, _ := os.ReadFile(filepath.Join(gitDirOf(root), gitHeadFile))
	branch, isBranch := strings.CutPrefix(strings.TrimSpace(string(head)), gitBranchRef)
	if !isBranch {
		return ""
	}
	return branch
}

func checkoutRoot(cwd string) string {
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return ""
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, gitEntry)); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func gitDirOf(checkout string) string {
	entry := filepath.Join(checkout, gitEntry)
	info, err := os.Stat(entry)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return entry
	}
	target, isPointer := strings.CutPrefix(firstLine(entry), gitFilePrefix)
	if !isPointer {
		return ""
	}
	target = strings.TrimSpace(target)
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(checkout, target)
}

func commonDirOf(checkout string) string {
	gitDir := gitDirOf(checkout)
	if gitDir == "" {
		return ""
	}
	relative := firstLine(filepath.Join(gitDir, gitCommonDirFile))
	if relative == "" {
		return gitDir
	}
	if filepath.IsAbs(relative) {
		return relative
	}
	return filepath.Join(gitDir, relative)
}

func firstLine(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(content), "\n")
	return strings.TrimSpace(line)
}
