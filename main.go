package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
	"github.com/rubentanahara/agent-trail/internal/tui"
)

const (
	defaultReplayDays = 2.0
	hoursPerDay       = 24
	exitUsage         = 2
)

type options struct {
	once bool
	all  bool
	days float64
	repo string
}

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		os.Exit(exitUsage)
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "agent-trail:", err)
		os.Exit(1)
	}
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("agent-trail", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: agent-trail [--once] [--days N | --all] [--repo PATH]")
		fmt.Fprintln(output, "Live, filterable log of what Claude Code, Codex and Copilot CLI do in a git repository.")
		flags.PrintDefaults()
	}
	flags.BoolVar(&opts.once, "once", false, "print the history as plain text and exit")
	flags.BoolVar(&opts.all, "all", false, "replay the whole history")
	flags.Float64Var(&opts.days, "days", defaultReplayDays, "days of history to replay")
	flags.StringVar(&opts.repo, "repo", "", "repository to follow (default: the git repository of the current directory)")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(output, "unexpected argument: %s\n", flags.Arg(0))
		return opts, errors.New("unexpected argument")
	}
	if opts.all && isFlagSet(flags, "days") {
		fmt.Fprintln(output, "--all and --days cannot be combined")
		return opts, errors.New("conflicting flags")
	}
	return opts, nil
}

func isFlagSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func run(opts options) error {
	repo, err := repository(opts.repo)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cutoff := time.Time{}
	if !opts.all {
		cutoff = time.Now().Add(-time.Duration(opts.days * hoursPerDay * float64(time.Hour)))
	}
	follower := trail.New(home, repo)
	history := follower.Start(cutoff)
	if opts.once || !isTerminal(os.Stdout) {
		return printHistory(os.Stdout, history.Events)
	}
	return tui.Run(follower, history, repo)
}

func repository(override string) (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	if override != "" {
		command.Dir = override
	}
	out, err := command.Output()
	if err != nil {
		return "", errors.New("run it inside a git repository or pass --repo")
	}
	return strings.TrimSpace(string(out)), nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func printHistory(out io.Writer, events []trail.Event) error {
	writer := bufio.NewWriter(out)
	for _, event := range events {
		fmt.Fprintf(writer, "%s %s %s %s\n", event.Time.Local().Format(time.DateTime), event.Who(), event.Kind, event.Summary)
	}
	if err := writer.Flush(); err != nil && !errors.Is(err, os.ErrClosed) && !isBrokenPipe(err) {
		return err
	}
	return nil
}

func isBrokenPipe(err error) bool {
	return strings.Contains(err.Error(), "broken pipe")
}
