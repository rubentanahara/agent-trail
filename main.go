package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
	"github.com/rubentanahara/agent-trail/internal/trail"
	"github.com/rubentanahara/agent-trail/internal/tui"
)

const (
	devVersion        = "dev"
	unstampedVersion  = "(devel)"
	modulePrefixLabel = "v"
)

var version = devVersion

const (
	defaultReplayDays = 2.0
	statsDays         = 30
	hoursPerDay       = 24
	exitUsage         = 2
)

type options struct {
	once    bool
	all     bool
	days    float64
	repo    string
	offline bool
	prices  bool
	version bool
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
		fmt.Fprintln(output, "Usage: agent-trail [--once] [--days N | --all] [--repo PATH] [--offline] [--prices] [--version]")
		fmt.Fprintln(output, "Live, filterable log of what Claude Code, Codex and Copilot CLI do in a git repository.")
		flags.PrintDefaults()
	}
	flags.BoolVar(&opts.once, "once", false, "print the history as plain text and exit")
	flags.BoolVar(&opts.all, "all", false, "replay the whole history")
	flags.Float64Var(&opts.days, "days", defaultReplayDays, "days of history to replay")
	flags.StringVar(&opts.repo, "repo", "", "repository to follow (default: the git repository of the current directory)")
	flags.BoolVar(&opts.offline, "offline", false, "do not download the price list; use the cached copy if there is one")
	flags.BoolVar(&opts.prices, "prices", false, "print the price of every model used in the replayed history and exit")
	flags.BoolVar(&opts.version, "version", false, "print the version and exit")
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
	if opts.version {
		fmt.Println("agent-trail", buildVersion())
		return nil
	}
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
	prices, status := pricing.Load(context.Background(), pricing.DefaultOptions(opts.offline))
	follower := trail.New(home, repo, prices)
	history := follower.Start(cutoff)
	if opts.prices {
		return printPrices(os.Stdout, prices, status, history.Events)
	}
	if status.Warning != "" && (opts.once || !isTerminal(os.Stdout)) {
		fmt.Fprintln(os.Stderr, "agent-trail:", status.Warning)
	}
	if opts.once || !isTerminal(os.Stdout) {
		return printHistory(os.Stdout, history.Events)
	}
	return tui.Run(follower, history, tui.Context{
		Repo: repo, Home: home, Version: buildVersion(), PricesAsOf: prices.FetchedAt, PriceWarning: status.Warning,
		Everywhere: trail.New(home, "", prices), StatsSince: statsSince(opts.all, cutoff),
	})
}

func buildVersion() string {
	if version != devVersion {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == unstampedVersion {
		return version
	}
	return strings.TrimPrefix(info.Main.Version, modulePrefixLabel)
}

func statsSince(all bool, cutoff time.Time) time.Time {
	if all {
		return time.Time{}
	}
	monthAgo := time.Now().AddDate(0, 0, -statsDays)
	if cutoff.Before(monthAgo) {
		return cutoff
	}
	return monthAgo
}

func printPrices(out io.Writer, prices pricing.Table, status pricing.Status, events []trail.Event) error {
	fmt.Fprintf(out, "price list: %s (fetched %s)\n", status.Source, prices.FetchedAt)
	if status.Override != "" {
		fmt.Fprintf(out, "overrides:  %s\n", status.Override)
	}
	if status.Warning != "" {
		fmt.Fprintf(out, "warning:    %s\n", status.Warning)
	}
	fmt.Fprintf(out, "\n%-32s %-9s %10s %10s %10s %10s %10s\n", "model (USD per million tokens)", "source", "input", "output", "cache read", "write 5m", "write 1h")
	seen := map[string]bool{}
	for _, event := range events {
		if event.Usage == nil || seen[event.Usage.Model] {
			continue
		}
		seen[event.Usage.Model] = true
		price, ok := prices.Lookup(event.Usage.Model)
		if !ok {
			fmt.Fprintf(out, "%-32s %-9s %s\n", event.Usage.Model, "-", "no price, costs not shown")
			continue
		}
		base := price.Base
		fmt.Fprintf(out, "%-32s %-9s %10s %10s %10s %10s %10s\n", event.Usage.Model, price.Source,
			perMillion(base.Input), perMillion(base.Output), perMillion(base.CacheRead), perMillion(base.CacheWrite5m), perMillion(base.CacheWrite1h))
	}
	return nil
}

func perMillion(rate *float64) string {
	if rate == nil {
		return "-"
	}
	return fmt.Sprintf("%.4g", *rate*1_000_000)
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
