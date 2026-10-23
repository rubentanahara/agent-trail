package gh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestCommandIsBuiltWithoutShellAndWithQuietEnvironment(t *testing.T) {
	command := newCommand(context.Background(), "/work", []string{"pr", "list", "--limit=100"})

	if filepath.Base(command.Path) != "gh" && command.Err == nil {
		t.Fatalf("Path = %q, want gh", command.Path)
	}
	if !slices.Equal(command.Args[1:], []string{"pr", "list", "--limit=100"}) {
		t.Fatalf("Args = %v", command.Args)
	}
	if command.Dir != "/work" {
		t.Fatalf("Dir = %q", command.Dir)
	}
	if command.WaitDelay != waitDelay {
		t.Fatalf("WaitDelay = %v", command.WaitDelay)
	}
	if command.Stdin != nil {
		t.Fatalf("Stdin = %v, want unset", command.Stdin)
	}
	for _, want := range []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1"} {
		if !slices.Contains(command.Env, want) {
			t.Fatalf("Env is missing %s", want)
		}
	}
}

func TestExecWithMissingBinaryIsClassifiedMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Exec(os.TempDir())(context.Background(), []string{"--version"})

	if err == nil {
		t.Fatal("expected an error")
	}
	if got := ProblemOf(err); got != Missing {
		t.Fatalf("ProblemOf = %v, want Missing", got)
	}
	var runErr *Error
	if !errors.As(err, &runErr) || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %#v, want *Error wrapping exec.ErrNotFound", err)
	}
}

func TestErrorMessageNamesArgsAndStderr(t *testing.T) {
	err := &Error{Args: []string{"pr", "view", "7"}, Stderr: "boom\n", Err: errors.New("exit status 1")}

	if got, want := err.Error(), "gh pr view 7: boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
