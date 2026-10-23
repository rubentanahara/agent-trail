# agent-trail

A terminal UI that shows, live, everything Claude Code, OpenAI Codex CLI and GitHub Copilot CLI do in a git repository: prompts, replies, tool calls and their results, permissions, hooks, subagents, token usage and cost.

It reads the transcripts each CLI already writes to disk. Nothing is sent anywhere, and nothing has to be configured in the agents.

```
╭ Sessions (12) ──────────────────╮╭ Events: all sessions (2992) · following ──────────────────────────╮
│▶ All sessions            2992 ev││10-22 21:15:39 claude      user       fix the login redirect         │
│  ● Fix login redirect    ~$14.09││10-22 21:15:46 claude      tool       Bash command: make check       │
│  ● Report checks          1% lim││10-22 21:15:58 claude      result     FAILED exit 2 …                │
│  ● Create Broken.cs        1 req││10-22 21:16:02 codex       user       write tests for the parser     │
╰─────────────────────────────────╯╰───────────────────────────────────────────────────────────────────╯
╭ Session af572da6 ───────────────╮╭ Detail (r: raw JSON) ─────────────────────────────────────────────╮
│Model    claude-opus-5-5         ││2026-10-22 21:15:58 · claude · result                               │
│Billing  subscription            ││FAILED exit 2                                                       │
│Tokens   in 41.3M · out 441k     ││error CS1513: } expected                                            │
│Cost     ~$14.09 API-equivalent  ││                                                                    │
╰─────────────────────────────────╯╰───────────────────────────────────────────────────────────────────╯
```

Claude Code is orange, Codex green and Copilot purple. Your own prompts are bold cyan.

## Install

```sh
go install github.com/rubentanahara/agent-trail@latest
```

Requires Go 1.26 or newer. The binary lands in `$(go env GOBIN)` or `~/go/bin`.

## Use

Run it inside the repository you want to watch:

```sh
agent-trail              # last 2 days of history, then follow live
agent-trail --days 7     # replay 7 days
agent-trail --all        # replay everything
agent-trail --repo ~/src/app
agent-trail --once | grep FAILED   # plain text, no TUI
```

When stdout is not a terminal it prints the history as plain text and exits.

| Key | Action |
|---|---|
| `tab` / `shift+tab` | Move between the sessions, events and detail panes |
| `↑` `↓` `j` `k`, `pgup` `pgdown`, `g` `G` | Move |
| `enter` | Sessions: open the session's events. Events: open the detail pane |
| `/` | Fuzzy filter. Every word must match; `'word` matches exactly; `!word` excludes |
| `esc` | Leave the detail pane, clear the filter, then go back to all sessions |
| `s` | Show only the session of the selected event |
| `a` | Show all sessions |
| `u` | Show or hide token-usage rows |
| `r` | Detail pane: switch between readable text and the raw JSON record |
| `f` | Follow new events |
| `q` | Quit |

Mouse: the wheel scrolls the pane under the pointer, a click selects a row.

Filter examples: `'codex 'tool`, `failed`, `!attachment !usage`, `'claude/web-fetch`.

## What it shows

**Sessions.** One row per session that had activity in the replay window, newest first. The name is the session title the CLI generated or you set, otherwise your first prompt. Subagent activity is shown inside its parent session as `claude/<agent-type>`.

**Per session:** agent, subagents, model, git branch, permission mode, billing, active time, tokens in (with cache reads and writes), tokens out (with reasoning), cost, rate-limit and context use, lines added and removed, number of model calls.

**Cost and billing.** Each CLI records this differently:

| | Billing detected from | Cost shown |
|---|---|---|
| Claude Code | The `oauth-2025-04-20` beta on recorded API requests: present means a Claude subscription, absent means an API key | Computed per model call from Anthropic's API list prices. With a subscription it is labelled `~$… API-equivalent`; with an API key it is shown in red as billed |
| Codex | `plan_type` in the rate-limit data: a ChatGPT plan, or none for an API key | Rate-limit use for a plan. No dollar figure, because no OpenAI price table is built in |
| Copilot | `isByok` on each model call: bring-your-own-key, or the Copilot plan | Premium requests used |

Claude prices are a table in [`internal/trail/pricing.go`](internal/trail/pricing.go), dated with `ClaudePricesAsOf`. The computation matches Claude Code's own recorded total exactly for Opus 5.5 sessions. Cache-read prices for models other than Opus 5.5, Sonnet 5.5 and Fable 5.1 assume 10% of the input price. Models missing from the table are counted as unpriced rather than as free.

## Where the data comes from

| CLI | Transcripts | Session name |
|---|---|---|
| Claude Code | `~/.claude/projects/<repo path>*/*.jsonl` and `*/subagents/*.jsonl` | `custom-title`, `agent-name` or `ai-title` entries |
| Codex | `~/.codex/sessions/**/rollout-*.jsonl` | `~/.codex/session_index.jsonl` |
| Copilot CLI | `~/.copilot/session-state/*/events.jsonl` | `workspace.yaml` |

A session belongs to the repository when the working directory it recorded is the repository or a folder inside it. Sibling folders with the same prefix are excluded.

These formats are not documented by their vendors and can change in any CLI release. A record of a known type whose fields no longer decode is shown as `unparsed` rather than dropped, so format changes are visible.

## Develop

```sh
go test -race ./...
go vet ./...
```

## License

MIT
