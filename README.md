# agent-trail

A terminal UI that shows, live, everything Claude Code, OpenAI Codex CLI and GitHub Copilot CLI do in a git repository: prompts, replies, tool calls and their results, permissions, hooks, subagents, token usage, cost, and whether each session is still running.

It reads the transcripts each CLI already writes to disk. Nothing has to be configured in the agents.

```
╭ Sessions (12) ──────────────────────────╮╭ Events: Fix login redirect (686) · ● running · busy · auto-scroll ─╮
│▶ All sessions                    2992 ev ││10-22 21:15:39 claude      user       fix the login redirect          │
│  ● Fix login redirect        2s ~$14.09  ││10-22 21:15:46 claude      tool       Bash command: make check        │
│  ◌ Report checks            12m ~$0.0009 ││10-22 21:15:58 claude      result     FAILED exit 2 …                 │
│  ○ Create Broken.cs          3h    1 req ││10-22 21:16:02 claude      assistant  The build fails because …       │
╰──────────────────────────────────────────╯╰────────────────────────────────────────────────────────────────────╯
╭ Session af572da6 ────────────────────────╮╭ Detail (r: raw JSON) ──────────────────────────────────────────────╮
│State    running · busy · pid 38322       ││2026-10-22 21:15:58 · claude · result                                │
│Model    claude-opus-5-5                  ││FAILED exit 2                                                        │
│Billing  subscription                     ││error CS1513: } expected                                             │
│Cost     ~$14.09 API-equivalent           ││                                                                     │
╰──────────────────────────────────────────╯╰────────────────────────────────────────────────────────────────────╯
```

Claude Code is orange, Codex green and Copilot purple. Your own prompts are bold cyan.

## Install

Download a binary for macOS or Linux from the [releases page](https://github.com/rubentanahara/agent-trail/releases), extract it and put `agent-trail` on your `PATH`.

Or build it with Go 1.26 or newer:

```sh
go install github.com/rubentanahara/agent-trail@latest
```

## Use

Run it inside the repository you want to watch:

```sh
agent-trail              # last 2 days of history, then follow live
agent-trail --days 7     # replay 7 days
agent-trail --all        # replay everything
agent-trail --repo ~/src/app
agent-trail --once | grep FAILED   # plain text, no TUI
agent-trail --prices     # the price used for every model in the history
agent-trail --offline    # do not download the price list
agent-trail --version
```

When stdout is not a terminal it prints the history as plain text and exits.

### Keys

| Key | Action |
|---|---|
| `1` `2` `3`, `tab`, `shift+tab` | Focus the sessions, events or detail pane |
| `↑` `↓` `j` `k`, `pgup` `pgdown`, `g` `G` | Move. In the sessions pane, moving selects the session whose events are shown |
| `enter` | Sessions: go to its events. Events: go to the detail |
| `/` | Filter the focused pane (see below) |
| `esc` | Clear the focused pane's filter, leave the detail pane, then go back to all sessions |
| `s` | Show only the session of the selected event |
| `a` | Show all sessions |
| `n` | Show or hide noise: Claude Code attachment and system rows. Hidden at start |
| `u` | Show or hide token-usage rows |
| `r` | Detail pane: readable text or the raw JSON record, indented and coloured |
| `f` | Turn auto-scroll to the newest event on or off |
| `z` | Zoom the focused pane to the whole screen, or restore the layout. While zoomed, `1` `2` `3` and `tab` switch which pane fills the screen |
| `o` | Sessions pane: show only running sessions, or all again. Codex sessions never show as running (see State) |
| `x` | Sessions pane: delete the selected session and its transcript files, after you confirm with `y`. Running sessions cannot be deleted |
| `t` | Open or close the stats screen (see below) |
| `q` | Quit |

Mouse: the wheel scrolls the pane under the pointer, a click selects a row.

### Filtering

`/` filters whichever pane has focus, and each pane keeps its own filter. The pane title shows the active filter, and `esc` clears it.

| Pane | What `/` does |
|---|---|
| Sessions | Keeps sessions whose name, CLI, model, agent, branch, state or billing match |
| Events | Keeps events whose CLI, agent, kind or text match |
| Detail | Keeps only the lines of the selected event that match your words |

Query syntax, the same in every pane:

| Query | Matches |
|---|---|
| `make chk` | Every word must match, fuzzily: its letters appear in order (`chk` matches `check`). The matched letters are highlighted |
| `'codex` | Exact text |
| `!attachment` | Excludes rows containing the text |

Examples: `'codex 'tool`, `'failed`, `'claude/web-fetch`, `!usage`. In the sessions pane, `running` or `'api` find live sessions or API-billed ones.

### Deleting a session

`x` removes these files, and only after `y`:

| CLI | Removed |
|---|---|
| Claude Code | `~/.claude/projects/<project>/<id>.jsonl` and the `<id>/` folder beside it (subagents, tool results) |
| Codex | the session's `rollout-*.jsonl`. Its name stays in `~/.codex/session_index.jsonl` |
| Copilot CLI | `~/.copilot/session-state/<id>/` |

The deletion is permanent; nothing goes to the trash.

### Stats screen

`t` opens a separate screen with charts across every repository, not only the one you follow. It reads the last 30 days, or the whole history with `--all`, and takes a few seconds the first time.

| Chart | Measure |
|---|---|
| Which CLI you use most | Prompts you typed, with sessions, cost and the share of input tokens read from the prompt cache |
| Which project you work on most | Prompts per git repository, found from each session's working directory |
| Which file types the agents touch | Tool calls that name a file (`file_path`, `notebook_path`, `path`, Codex file changes), by extension |
| Spend | Cost of the current minute, hour, day, week and month, the average per period over the window, and bars for the last 60 minutes, 24 hours and 30 days |
| Tokens your tools saved | Savings that token-saving tools record about themselves (see below) |

Costs use API list prices, so subscription usage is an API-equivalent figure.

Sessions started by a program rather than typed in the interactive CLI are hidden at start, and the header says how many. `a` shows or hides them. Claude Code marks them with an `sdk-*` entrypoint (`claude -p`, the Agent SDK, tools such as claude-mem's observer); Codex marks them with the `exec` source (`codex exec`). Copilot CLI records no such marker, so its sessions are always shown.

#### Token savings

Only tools that write down what they saved appear, with their own figures:

| Tool | Read from | Per CLI |
|---|---|---|
| [Headroom](https://github.com/headroomlabs-ai/headroom) | `~/.headroom/savings_events.jsonl`: tokens removed from each proxied request and Headroom's dollar estimate | Yes, from the `client` field |
| [RTK](https://github.com/rtk-ai/rtk) | `rtk gain --daily --format json`: tokens removed from command output | No. RTK does not record which CLI ran a command, so it shows as `any CLI` |

Output-style tools such as caveman and ponytail record no savings: there is no run without them to compare with, and their own documentation says so. They are not shown.

## What it shows

**Sessions.** One row per session that had activity in the replay window, newest first. Each row shows its state, the time since its last event, and its cost. The name is the session title the CLI generated or you set, otherwise your first prompt. Subagent activity is shown inside its parent session as `claude/<agent-type>`.

**Session state.**

| Mark | Meaning | How it is known |
|---|---|---|
| `●` | Running, with `busy`, `idle` or `working` when known | Claude Code: `~/.claude/sessions/<pid>.json` names the session and its process is alive. Copilot: the session folder holds `inuse.<pid>.lock` and that process is alive |
| `○` | Closed, including sessions ended with `ctrl+c` | The marker above is gone or its process is dead |
| `◌` | Not recorded | Codex keeps no per-session marker on disk. `working` is shown while one of its turns is open |

**Per session:** state, agent, subagents, model, git branch, permission mode, billing, active time, tokens in (with cache reads and writes), tokens out (with reasoning), cost, rate-limit and context use, lines added and removed, number of model calls.

## Cost and billing

| | Billing detected from | Shown |
|---|---|---|
| Claude Code | The `oauth-2025-04-20` beta on recorded API requests: present means a Claude subscription, absent means an API key | Cost per model call. Also the total Claude Code records for the session, labelled `Reported` |
| Codex | `plan_type` in the rate-limit data: a ChatGPT plan, or none for an API key | Cost per model call and rate-limit use |
| Copilot | `isByok` on each model call: bring-your-own-key, or the Copilot plan | Premium requests, and cost per model call |

With a subscription the cost is labelled `~$… API-equivalent`: what the same tokens would cost at the provider's API list price. Billed to an API key, it is shown in red.

**Prices are not built in.** They come from [LiteLLM's public price list](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json) (Anthropic and OpenAI entries only):

- Downloaded on first run and cached in your user cache folder (`~/Library/Caches/agent-trail` on macOS, `~/.cache/agent-trail` on Linux).
- Refreshed in the background once the cache is a day old. The download is a plain GET of that public file; nothing about your sessions is sent.
- When a model is missing from the list, or the list lacks the rate for a kind of token the call used (for example cache writes), that call's cost is not shown. It is never guessed.
- `--offline` uses the cached copy only. `--prices` prints the price used for each model in your history.

To correct or add a price, create `~/.config/agent-trail/prices.json` in the same format. Its entries replace the list's:

```json
{
  "claude-sonnet-5-5": {
    "input_cost_per_token": 2e-06,
    "output_cost_per_token": 1e-05,
    "cache_read_input_token_cost": 2e-07,
    "cache_creation_input_token_cost": 2.5e-06,
    "cache_creation_input_token_cost_above_1hr": 4e-06
  }
}
```

The example is a real case: on 2026-10-22 the list had $0.10 per million cache-read tokens for Claude Sonnet 5.5, while the totals Claude Code itself records imply $0.20. The `Reported` row makes such differences visible.

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
golangci-lint run ./...      # linters and formatter checks, configured in .golangci.yml
golangci-lint fmt ./...      # apply gofumpt and goimports
```

Pushing a `v*` tag builds and publishes release binaries with GoReleaser.

## License

MIT
