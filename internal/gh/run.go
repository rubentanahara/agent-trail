package gh

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ReadTimeout  = 30 * time.Second
	WriteTimeout = 2 * time.Minute
	waitDelay    = 5 * time.Second
)

var quietEnvironment = []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1"}

type Runner func(ctx context.Context, args []string) ([]byte, error)

type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	message := "gh " + strings.Join(e.Args, " ")
	if stderr := strings.TrimSpace(e.Stderr); stderr != "" {
		return message + ": " + stderr
	}
	return message + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func Exec(dir string) Runner {
	return func(ctx context.Context, args []string) ([]byte, error) {
		var stdout, stderr bytes.Buffer
		command := newCommand(ctx, dir, args)
		command.Stdout = &stdout
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			return nil, &Error{Args: args, Stderr: stderr.String(), Err: err}
		}
		return stdout.Bytes(), nil
	}
}

func newCommand(ctx context.Context, dir string, args []string) *exec.Cmd {
	command := exec.CommandContext(ctx, "gh", args...)
	command.Dir = dir
	command.WaitDelay = waitDelay
	command.Env = append(os.Environ(), quietEnvironment...)
	return command
}
