# PRD: GitHub Pull Request screen for agent-trail

**Author:** rubentanahara
**Team:** rubentanahara

| Role | Name |
|---|---|
| Product Manager | rubentanahara |
| Engineering Lead/Team Lead | rubentanahara |
| Designer | |
| Approvers/Sign-Off | |

**PM Epic:** n/a (personal project)
**Status of PRD:** Implemented on main, unreleased

---

## One Pager

### Overview

agent-trail is a Go and Bubble Tea v2 terminal UI that shows live Claude Code, Codex and Copilot sessions in a git repository. This PRD adds a GitHub Pull Request screen, opened with `p`, to the same binary. It covers the current repository only.

The screen has a PR list on the left and a detail pane on the right. The detail pane has five tabs: Overview, Checks, Comments, Diff and Sessions. It reads and writes PR state through the `gh` CLI, including `gh api graphql` for review threads, resolve and pending reviews. It never calls the GitHub API directly and never handles a token.

What sets it apart from gh-dash is the session link. A PR is matched to the agent sessions that produced its branch, so the user sees which sessions built the PR, what they cost, and whether they are still running.

Delivery is in three waves, each its own release:

| Wave | Release | Scope |
|---|---|---|
| 1 | v0.5 | Read-only |
| 2 | v0.6 | Common write actions, check logs, re-run |
| 3 | v0.7 | Line comments and pending reviews |

### Problem

Developers who run coding agents have two views of the same work, and they are apart. agent-trail knows what the agent did, what it cost and whether it is still running. GitHub knows whether the result is reviewed, green and mergeable. Today the developer leaves the terminal, finds the PR in a browser, and reconciles the PR with the session by hand. No existing tool does that reconciliation.

- Reviewing a PR, answering review threads, checking CI and merging all need a browser or a second TUI.
- No tool links a PR to the agent sessions and the cost behind it.

The evidence for this problem is the maintainer's own workflow. There is no wider demand data.

### Objectives

1. **Close the loop.** A user can go from "my agent finished" to "PR merged" without leaving agent-trail.
2. **Own the session link.** Every PR with a matching branch shows its session count, cost and state. Every session row shows its PR badge, and each side can jump to the other.
3. **Ship trustworthy increments.** Each wave is releasable alone, deterministic, covered by tests against a fake `gh`, and clean under the race detector.

### Constraints

1. **Single repository.** The current repository only. There is no multi-repo configuration.
2. **`gh` dependency.** The feature is as capable as the user's installed `gh` and its authentication.
3. **Isolation.** A missing `gh`, missing authentication or a repo with no GitHub remote shows an inline message. The rest of the app works unchanged.
4. **Rate budget.** GitHub allows 5000 REST requests and 5000 GraphQL points per hour per account, with 30 search requests per minute and secondary limits on bursts. Polling is limited to 30 seconds, only while the screen is visible.
5. **Partial renderer reuse.** The Diff tab reuses the Files diff styling and layout helpers. The Files block headers and change counter do not apply to a git unified diff.
6. **Test cost.** Every behaviour needs a fixture-driven test, so fixture capture is part of each feature's cost.
7. **Platforms.** macOS and Linux only, as shipped today. Windows is not a target.

### Persona

| Persona | Description |
|---|---|
| Key Persona: Agent-driving developer | Runs coding agent sessions in a repo, opens PRs from agent work, and wants one terminal view of session and PR. This is the existing agent-trail user. |
| Reviewer | Reviews teammates' PRs, some written by agents. Needs diff, threads, checks and review submit. |
| Maintainer / merger | Triages open PRs, watches CI, re-runs flaky checks and merges. |

### Use Cases

**Scenario 1: Agent finished, ship it.** A session on branch `feat/x` goes idle. The developer presses `p`. My PRs shows the PR with a session badge, a count and a cost. Checks show one failing Actions job. They open its log, find the error, and re-run failed jobs. The check turns green, so they merge with squash and delete-branch, and confirm with `y`.

**Scenario 2: Review a teammate's PR.** The reviewer opens Needs my review and reads the Diff tab. They comment on a right-side line, add more comments to the same pending review, press `S`, and submit "Request changes".

**Scenario 3: Answer feedback on my PR.** The developer sees unresolved threads on their PR, replies to one, edits their own comment and resolves the thread. The Sessions tab shows the session that wrote the code, and `enter` jumps to it so they can ask the agent to fix the rest.

---

## PRD

### Features In

#### Wave 1: v0.5, read-only

- **W1-1 `gh` runner (Critical).** One abstraction for running `gh`, with a fake backed by fixture JSON. It classifies the degraded states.
- **W1-2 PR list (Critical).** Sections Needs my review, My PRs and All open for the current repo. The `p` screen, 30 second visible-only polling, `R` refresh.
- **W1-3 Detail and Overview tab (Critical).** Title, body as Markdown, state, author, branch, mergeability and review status. Tabs switch with `[` and `]`.
- **W1-4 Checks tab (High).** All checks from the status rollup with state and duration. Open a check in the browser.
- **W1-5 Comments tab (High).** PR-level comments and review threads, read-only, with resolved state and inline threads under their file.
- **W1-6 Diff tab (High).** Read-only unified diff for the PR.
- **W1-7 Sessions link (High).** The differentiator. See Session matching below.
- **W1-8 Degraded states (Critical).** Inline message for each problem.
- **W1-9 Branch history (High).** `trail` records the set of branches each session has been on, from per-event data and from sampling the live checkout while the session runs. Required by W1-7.

#### Wave 2: v0.6, common writes

- **W2-1 Reply** to a thread or PR comment (High).
- **W2-2 Resolve and unresolve** a thread (High).
- **W2-3 Edit** own comments, PR title and PR body in `$EDITOR`, or open in the browser (Medium).
- **W2-4 Close** a PR, with `y` confirm (Medium).
- **W2-5 Merge (High).** Method picker limited to repo-allowed methods, a delete-branch toggle, auto-merge when checks are pending, a warning without a block when checks fail, `y` confirm.
- **W2-6 One-shot review (High).** Approve, request changes or comment, body from `$EDITOR`.
- **W2-7 Check logs (High).** Scrollable failed-job log for GitHub Actions checks. `a` toggles the full log, capped at the last 5000 lines. ANSI codes and timestamps are stripped. `/` searches.
- **W2-8 Re-run (Medium).** Failed jobs or all jobs, `y` confirm. Actions checks only.

#### Wave 3: v0.7, inline review

- **W3-1 Line comments (High).** On a single right-side line of the Diff tab.
- **W3-2 Pending reviews (High).** Continue a pending review started on the web, show Pending (N), submit with `S`, discard with confirm.

#### Session matching

- A session matches a PR when the PR's `headRefName` is in the session's branch set, the PR is not cross-repository, and the branch is not empty.
- The branch set is the union of: branches recorded in the session's events, and branches sampled from the live checkout while the session runs. A branch stays in the set after the session switches away from it. After the session closes, only recorded and previously sampled branches count.
- Branch name equality covers sessions in other worktrees on that branch.
- If several PRs share a head branch, the badge shows the most recently updated one plus a `+N`.
- Claude Code records the branch on every event. Codex and Copilot record it once at start, so they rely on sampling for mid-session switches. A session that switched branch and closed before sampling saw it is a known limitation.
- Sessions tab: sessions with count, summed cost and state. `enter` jumps to the session in the main view. A hidden session (automated filter, running-only filter) shows a notice instead.
- Session rows show `#N`. At 100 columns or wider they also show a review or checks glyph.
- `p` on a selected session with a matching PR opens the screen on that PR. Otherwise it opens the list.
- The badge needs PR data while the screen is closed, so the list is fetched once at startup and refreshed whenever the screen is open. It is skipped when `gh` is unavailable.

### Features Out

- **Multi-repo or org-wide PR views.** The key persona works in one repo, and it avoids a configuration surface.
- **Direct GitHub API client.** `gh` owns authentication and transport.
- **Range comments and suggestion blocks.** Costly, in the largest wave.
- **Left-side (deleted line) comments.** Not needed to review added code.
- **Diff editing or checkout.**
- **Logs and re-run for non-Actions checks.** There is no common API. They open in the browser.
- **PR creation.** Not part of the review loop. A possible later feature.
- **GitHub Enterprise Server and non-GitHub hosts.** Not tested and not promised.
- **Windows.**
- **Telemetry.** agent-trail collects none.

### Requirements

| ID | Requirement | Acceptance criteria | Priority |
|---|---|---|---|
| R1 | `p` opens the PR screen. In global mode (every repository) it shows a notice instead. | Given the main screen in a repo, when the user presses `p`, then the list and detail panes show. Given global mode, when the user presses `p`, then a notice shows and the screen stays closed. | Critical |
| R2 | The list shows Needs my review, My PRs and All open. | Given a fixture with PRs in each category, when the screen loads, then each PR is under the right section with a count. Empty sections are hidden. | Critical |
| R3 | Tabs are Overview, Checks, Comments, Diff and Sessions, switched with `[` and `]`. | Given Overview active, when the user presses `]` four times, then Sessions is active. When the user presses `[` on Overview, then Sessions is active. | Critical |
| R4 | Polling runs every 30s only while the screen is visible. `R` refreshes at once. | Given the screen is visible and 31s pass, then one fetch happens. Given the screen is closed and 60s pass, then no fetch happens. Given a fetch in flight, when the poll fires, then it is skipped. | Critical |
| R5 | Degraded states show an inline remedy and do not affect other screens. | Given `gh` missing, unauthenticated, no GitHub remote, ambiguous remote, missing scope or outdated `gh`, when the user opens `p`, then each shows its own remedy line. The sessions screen is unchanged. | Critical |
| R6 | The Checks tab lists every entry of the status rollup and opens one in the browser. | Given a fixture with check runs and status contexts, then both kinds are listed with state. When the user presses `o`, then the browser opens its URL. | High |
| R7 | The Comments tab shows PR comments and review threads with resolved state. | Given resolved and unresolved threads, then each is labelled in words as well as colour. | High |
| R8 | The Diff tab renders the PR diff. | Given a diff fixture with a rename and a binary file, then file counts exclude `+++` and `---` headers and lines use the Files diff styles. A diff over GitHub's size limit shows "diff too large, open on GitHub". | High |
| R9 | Sessions link by branch set. | Given sessions whose branch set contains `feat/x` and a PR with that head, then the Sessions tab shows them with summed cost and state. Given a cross-repo PR or an empty branch, then there is no link. | High |
| R10 | Session rows show a PR badge, and jumps work in both directions. | Given a matching session, then the row shows `#N`. When `enter` is pressed on it in the Sessions tab, then the main view selects it. When `p` is pressed on a matching session, then the screen opens on its PR. | High |
| R11 | The user can reply to a thread or PR comment. | Given a thread, when the user writes a reply in `$EDITOR`, then one reply call is recorded and the thread shows it after refetch. An empty body cancels. | High |
| R12 | The user can resolve and unresolve a thread. | Given an unresolved thread, when the user presses `v`, then the state is resolved after refetch. Pressing `v` again reverts it. | High |
| R13 | The user can edit their own comments and the PR title and body, or open them in the browser. | Given own content, then edit sends an update. Given someone else's content, then edit is unavailable. The title is passed in `--flag=value` form so a leading `-` is safe. | Medium |
| R14 | Close requires `y`. | Given an open PR, when the user presses `X` then `y`, then close is called once. Any other key calls nothing. | Medium |
| R15 | Merge offers only repo-allowed methods, a delete-branch toggle and `y` confirm. | Given a repo that allows only squash, then only squash is listed. After `y`, merge is called with the chosen method, toggle and `--repo`, from a temp directory. | High |
| R16 | Merge offers auto-merge when checks are pending and warns without blocking when checks fail. | Given pending checks, then auto-merge is offered. Given failing checks, then a warning shows and `y` still merges. | High |
| R17 | The user can submit a one-shot review. | Given a PR, when the user picks approve, request changes or comment and confirms, then one review call is made with the body from a file. | High |
| R18 | Actions logs are scrollable, capped and clean. | Given a log with ANSI codes and timestamps, then neither shows. Given 8000 lines, then the full view holds exactly the last 5000 and says "last 5000 lines". `/` finds matches. A run still in progress shows its own message. Lines over 64 KiB are truncated. | High |
| R19 | Re-run for Actions checks, with `y`. Non-Actions checks only open the browser. | Given an Actions check, then re-run for failed or all calls once after `y`. Given a status context, then re-run is not offered. | Medium |
| R20 | The user can comment on a single right-side diff line. | Given a context or added line, then a thread is created at that path and line on side RIGHT. Given a removed line or a line outside any hunk, then a notice refuses. | High |
| R21 | The user can continue, submit and discard a pending review. | Given a pending review of 3 comments created on the web, then the header reads Pending (3). `S` submits. `D` asks for `y` and then discards. If the head moved since the diff loaded, then new comments are blocked until it reloads. | High |
| R22 | Write actions are confirmed, not repeated and not retried. | A second press of the same action while it is in flight does nothing. A failed write is shown with its stderr text and is never retried automatically. `q` during a write warns and needs a second `q`. | High |
| NFR1 | Determinism. | Every feature has a test through the fake runner and fixture JSON. No test calls real `gh` or the network. | Critical |
| NFR2 | Race safety. | `go test -race ./...` passes on every release. Command closures capture only immutable values. | Critical |
| NFR3 | Responsiveness. | Network calls never block key handling. A keypress redraws within 100 ms with the fake runner at 200 PRs. | High |
| NFR4 | Rate-limit safety. | A poll costs at most 3 `gh` calls. A closed screen costs 0, except the one startup fetch. On 403 or 429 the screen shows "rate limited, retry at HH:MM", pauses polling and refuses writes. Poll errors back off at 30s, 60s, 120s. | High |
| NFR5 | Secrets. | agent-trail reads, stores and logs no token. | High |
| NFR6 | Terminal width. | The screen renders at 80x24 without overlap. Under 70 columns it falls back to a single pane. | Medium |
| NFR7 | Terminal safety. | All text from GitHub has control characters and ANSI removed before display. | High |

### Design

Mockups are pending. This section fixes the flow, states and keys.

#### Screen map and focus

`p` opens a full-screen view like Files (`d`) and Stats (`t`). `esc` or `p` closes it and restores the previous selection. It uses the existing chrome: rounded `paneBox` borders, focus colour 75 against idle 240, a one-line header and a footer with the version at the right.

- Header: `Pull requests · owner/repo · Pending (N) · updated 12s ago`.
- Two panes, list and detail, switched with `tab`, `shift+tab`, `1` and `2`. `enter` on a list row focuses the detail. `esc` in the detail returns to the list. `esc` in the list closes the screen.
- `[` and `]` change the tab from either pane.
- Moving in the list loads that PR's detail. The loading text shows only after about 150 ms.
- `/` filters the focused pane with the existing query syntax. `z` zooms the focused pane.
- Overlays (merge dialog, confirmation, log viewer, picker) are modal, one at a time.

#### Key map

Rule: lowercase is navigation, view or reversible. A capital letter changes GitHub state or opens a dialog.

| Key | Action |
|---|---|
| `p` | Open or close. On a session with a PR, open that PR |
| `[` `]` | Previous and next tab |
| `R` | Refresh now |
| `r` | Reply to the selected thread (Comments tab only) |
| `e` | Edit own comment, title or body |
| `v` | Resolve or unresolve the selected thread |
| `M` | Merge dialog |
| `X` | Close PR |
| `A` | Review: approve, request changes or comment |
| `S` | Submit pending review (shown only when Pending is above 0) |
| `D` | Discard pending review (same condition) |
| `c` | Line comment (Diff tab, valid line only) |
| `o` | Open in browser |
| `L` | Open logs for the selected check |
| `a` | Toggle failed or full log (log viewer only) |
| `E` | Re-run (failed or all) |
| `/` | Filter, or search in the log viewer |
| `y` | Confirm. Any other key cancels. Confirmation captures all keys |

#### List rows

Review glyph (`✓` approved, `✗` changes requested, `○` none, `◐` review requested of me), checks glyph (`●` pending, `✓` pass, `✗` fail, `–` none), a `draft` word, `⚠` for a conflict, and a right-aligned session count. Every glyph has a word somewhere on screen, so no state depends on colour. Bots are faint and marked `[bot]`. Unicode fallbacks apply when icons are off.

#### States

| State | Behaviour |
|---|---|
| First load | `loading…` after 150 ms |
| Refresh | Header says `refreshing…`. Old content stays |
| Empty list or tab | Specific faint text, such as "No checks reported" |
| Error | Red one-line cause and a next step. Old content stays under a stale marker |
| Stale | After 90 s without a successful poll, the header says `stale 2m` |
| Rate limited | Header and footer say `rate limited · retry in 4m`. Polling pauses. Reads show cached data. Writes refuse with that message |
| In flight | Footer says `merging…`. The same action on the same target is ignored |
| Success | Footer notice, then refetch |
| Failure | Red text with cause, kept until a key press |
| Confirmation | A bold line naming the PR, action and options. `y` confirms, any other key cancels, `enter` is never a default |
| Pending review | `Pending (N)` in the header on every tab, `✎N` in the list, a `✎` gutter mark in the Diff |
| Degraded | A whole-pane message with one remedy, for example "gh is not signed in. Run gh auth login in another terminal, then press R." |

Polling never resets the cursor, tab, scroll position or an open dialog. A dialog holds a snapshot and warns "PR updated since you opened this" if the PR changed. Merge refetches checks before it opens.

#### Narrow terminals

At 100 columns or more the list takes about 35 to 40 percent and the tab labels are full. Between 70 and 99 the list narrows, titles truncate and tab labels shorten. Under 70 the screen is a single pane and the header shows `PR #12 · tab 3/5 Comments`. Dialogs that do not fit collapse to one footer line. Diff lines truncate with a `→` marker.

#### Friction risks and mitigations

1. Capital and lowercase keys differ (`m` and `M`, `r` and `R`). State-changing actions always confirm with `y`, and the confirmation names the PR and method.
2. A `$EDITOR` handoff loses context. The footer shows `waiting for editor…`, and on return a notice says what was sent or that an empty body cancelled. If `$EDITOR` is unset, `$VISUAL` then `vi` is used.
3. A pending review is easy to forget. The header shows it on every tab, and leaving the screen reminds "Pending (2) kept. S submits."
4. Line comments work on right-side lines only. The cursor shows unsupported lines faint, and `c` on one explains why.
5. Stale or rate-limited data can mislead a merge. The labels sit in the header, and merge refetches first.

### Technical Considerations

#### Facts from the codebase that shape the design

- Full screens are structs on `model` with a `shown` flag (`statsScreen`, `filesScreen`, `helpScreen` in `internal/tui/tui.go`), dispatched in `model.key` and rendered in `view.go`. The PR screen follows this pattern.
- `internal/tui/summary.go` is the pattern for shelling out. The runner is a function-typed field, defaulting to the real one, run through `exec.CommandContext` with a timeout, `WaitDelay` and `Dir` set to the temp directory. The name `fakeRunner` is taken in the tests, so the PR fake is called `fakeGH`.
- `internal/tui/resume.go` already uses `tea.ExecProcess` to hand the terminal to a child process. The editor flow reuses it.
- The only staleness checks are `detailStillWanted` and the `filePatchesMsg` path check. There is no request id type.
- A global 1 second `tick()` sends `pollMsg{}`. It carries no timestamp, and `age()` calls `time.Since`, so nothing time-based is deterministic yet.
- `trail.Session.Branch` is a single value. Claude Code writes the branch on every line, and Codex and Copilot write it once at start. `applyCheckoutBranches` in `internal/trail/worktree.go` then overwrites it with the live checkout `HEAD`. Wave 1 adds a per-session branch set, filled from events and from that sampling.
- `countChanges` in `internal/trail/changes.go` counts every line starting with `+` or `-`, so it would count git diff headers. The PR diff needs its own counter.
- CI runs golangci-lint and `go test -race ./...` on ubuntu only. There are no golden files today. `.goreleaser.yaml` builds darwin and linux only.
- `renderFiles` never shows `m.notice`. The PR footer must, or write errors are invisible.

#### Corrections to the `gh` plan

1. `gh pr diff` has no `--json` flag. It prints a unified diff. Per-file counts come from `gh pr view --json files`.
2. `gh pr checks` is dropped. `statusCheckRollup` in `gh pr view --json` returns check runs and status contexts, told apart by `__typename`.
3. A workflow re-run likely needs only `repo` on a classic token, or "Actions: read and write" on a fine-grained token, not the `workflow` scope. Confirm in Wave 2.
4. `addPullRequestReviewComment` is deprecated in the GraphQL schema. A new line comment uses `addPullRequestReviewThread` with `path`, `line` and `side: RIGHT`. Replies use `addPullRequestReviewThreadReply`. Check the live schema at the start of Wave 3.
5. `gh pr merge --delete-branch` run inside a checkout deletes the local branch and may switch branches. The head branch is often checked out in an agent worktree. Merge runs with `--repo=<nameWithOwner>` from the temp directory so only the remote branch is deleted.

#### Package layout

| Path | Wave | Contents |
|---|---|---|
| `internal/gh/run.go` | 1 | `type Runner func(ctx, args) ([]byte, error)`, `Exec(dir)` for the real `gh`, an `Error{Args, Stderr, Err}` type, timeout constants |
| `internal/gh/problem.go` | 1 | `Problem` enum: Missing, Unauthenticated, NoRemote, AmbiguousRemote, MissingScope, OutdatedGh, Other. `ProblemOf(err)` |
| `internal/gh/pr.go` | 1 | Typed `PR`, `Check`, `Review`, `Comment`, `File`. Pure argument builders and pure decoders. Thin `List` and `View` |
| `internal/gh/diff.go` | 1, 3 | `SplitDiff` with counts that skip headers. Wave 3 adds right-side line numbers from hunk headers |
| `internal/gh/actions.go` | 2 | Argument builders for comment, edit, close, merge, review, re-run. Repo settings decoding |
| `internal/gh/logs.go` | 2 | `TailLog` ring buffer with ANSI and timestamp stripping. Run and job ids from a check's details URL |
| `internal/gh/review.go` | 3 | GraphQL queries and mutations as constants. Thread and pending review decoders |
| `internal/gh/testdata/` | 1+ | Fixtures captured from real `gh`: list, view, diff, failed log, stderr samples, GraphQL responses |
| `internal/tui/prscreen.go`, `prview.go` | 1 | Screen state, key handling, messages, rendering, degraded rows |
| `internal/tui/practions.go`, `prlogs.go` | 2 | Confirm flow, editor compose, merge options, log viewer |
| `internal/tui/prreview.go` | 3 | Line cursor, pending review, threads |

Small changes elsewhere: `tui.go` (field, `p` key, dispatch), `view.go` (render branch, session badge, `ageAt`), `help.go` (PR screen section), `markdown.go` (take cache and width as parameters so the PR body has its own cache), `internal/trail` (session branch set and live sampling). `main.go` needs no change.

The runner is a function type, not an interface. It matches `summaryRunner`, the fake is a closure, and there is one real implementation.

#### Binding rules

**Process and security**

- TC-S1: Every `gh` call uses `exec.CommandContext("gh", args...)`, never a shell.
- TC-S2: User-controlled values go in `--flag=value` form. PRs are addressed by number, never by branch name.
- TC-S3: Body text goes through `--body-file=<path>`, or `-F body=@<path>` for `gh api graphql`, never argv.
- TC-S4: GraphQL variables use `-f` for strings and `-F name=@file` for files, never interpolated into the query.
- TC-S5: agent-trail never reads, stores, logs or passes a token.
- TC-S6: The child environment adds `GH_PROMPT_DISABLED=1` and `GH_NO_UPDATE_NOTIFIER=1`. Stdin is unset.
- TC-S7: All text from GitHub has control characters and ANSI removed before rendering (`withoutControls`, `ansi.Strip`).
- TC-S8: The editor temp file uses `os.CreateTemp` (mode 0600). It is deleted only after the write succeeds, and kept for retry on failure.
- TC-S9: The editor is `$VISUAL`, then `$EDITOR`, then `vi`, split with `strings.Fields`, run through `tea.ExecProcess` and never through a shell. CRLF is normalized to LF. An empty result cancels.

**Async and state**

- TC-A1: A `tea.Cmd` closure captures only immutable values, never `*model` or mutable maps.
- TC-A2: Every read result carries a `seq`. Results older than the latest for that slot are dropped.
- TC-A3: A completed write increases `generation`. Reads issued under an older generation are dropped, then the list and detail are refetched.
- TC-A4: No optimistic UI. What is shown comes from a fresh fetch.
- TC-A5: At most one list fetch and one detail fetch run at a time. A poll is skipped while one runs. `R` fetches immediately unless one is in flight.
- TC-A6: Reads use a context owned by the screen and cancelled on close. Each write gets its own `context.WithTimeout(context.Background(), …)`, because cancelling a merge halfway leaves the outcome unknown.
- TC-A7: A write result reaches `m.notice` even if the screen has closed. The PR footer shows `m.notice`.
- TC-A8: While a write is in flight for a PR and action, the same key does nothing.

**Polling and cost**

- TC-P1: `pollMsg` carries the time it fires. The PR screen takes its clock only from that value, with no `time.Now()` in its files.
- TC-P2: The PR screen starts no ticker of its own. On each `pollMsg` it fetches only if shown, nothing is in flight, and 30 s have passed.
- TC-P3: A poll costs at most three calls: the open PR list (limit 100), the `review-requested:@me` list, and a detail view of the selected PR only when its `updatedAt` or `headRefOid` changed or its checks are pending. "My PRs" is computed on the client from the viewer login, fetched once with `gh api user --cache=1h --jq .login`. The review-requested list uses the search API, so it counts toward the 30 per minute search limit. If the measured cost is a problem, replace it with a GraphQL `reviewRequests` query.
- TC-P4: Threads and logs are fetched only when their tab or view opens, and never by polling.
- TC-P5: Repo merge settings are read once per screen open through `gh api … --cache=1h`.
- TC-P6: Lists stop at 100 PRs. The title shows `100+` beyond that.

**Write safety**

- TC-W1: Merge, close, re-run and discarding a pending review ask for `y`. Any other key cancels. The prompt names the PR, the action and its options.
- TC-W2: The merge prompt lists only allowed methods, says whether the branch is deleted, warns on failing checks but accepts `y`, and offers `--auto` when checks are pending and auto-merge is allowed.
- TC-W3: Writes are never retried automatically. A `gh` error is shown through `commandFailure`-style text and triggers a refetch. "Already merged" and "already closed" resolve through that refetch.
- TC-W4: A line comment targets only a right-side line (context or added). Anything else refuses with a notice.
- TC-W5: `gh pr merge` runs with `--repo=<nameWithOwner>` and the temp directory as `Dir`.
- TC-W6: The viewer's pending review is looked up before one is created. It is created with `commitOID` equal to the `headRefOid` the diff was loaded from. If the head moved, comments are blocked until the diff reloads.
- TC-W7: `q` during an in-flight write warns and needs a second `q`.

**Degraded states**

- TC-D1: In global mode, `p` sets a notice and does not open the screen.
- TC-D2: Each `Problem` shows one remedy line inside the list pane. The rest of the app keeps working.
- TC-D3: Version and scope problems are detected from failures (`unknown flag`, HTTP 403 with "scope"). There is no upfront version gate. The fixtures record the `gh` version they were captured with, and that is the known-good version shown in the "update gh" message.

#### Test strategy

| Layer | What is tested | How |
|---|---|---|
| `internal/gh` decoders | JSON to typed models, including both rollup kinds, a null author, drafts and cross-repo PRs | Pure `decode*` functions against `testdata` |
| Argument builders | Exact argv, `--flag=value` form, `--body-file`, `--repo` on merge | Pure functions compared with `slices.Equal` |
| Errors | `ProblemOf` | Captured stderr fixtures and a missing binary |
| Process | `WaitDelay`, `Dir`, environment | Inspect the built `*exec.Cmd`, as the summary tests do |
| Diff and logs | File split, header-free counts, renames, binary files, right-side line numbers, 5000-line tail, timestamp and ANSI removal | Pure functions on captured fixtures |
| `tui` Update | Open and close, staleness, poll gating, write guards, confirm flows, notices | Send messages to `Update` with `press`, `sized` and `settle`. `fakeGH` maps the joined argv to a canned result, records each call and fails on an unknown argv |
| Time | Poll interval, "updated 3m ago" | Build `pollMsg{at: t0.Add(31s)}` directly. Views use `ageAt(at, now)` |
| Editor | Command resolution and compose result | A pure `editorCommand(path)`, tested with `t.Setenv`. Build `composedMsg` by hand. `tea.ExecProcess` is never run in tests |
| View | Layout, tabs, badges, degraded rows | Substrings on `ansi.Strip(m.render())` at a fixed size, plus at most two golden files (list with overview, and diff) with an `-update` flag |
| Live (optional) | Fixtures still match real `gh` | A test skipped unless `AGENT_TRAIL_GH_LIVE=1`. It never runs in CI |

Tests never sleep. Poll results in tests are asserted on state (`inFlight`, `seq`), because running the poll command from `Update`'s batch would call `trail.Poll` with a nil follower.

#### Rate limits

- A steady poll costs about 2 calls per 30 s when the selected PR is unchanged and 3 when it changed: at most about 360 calls an hour, against a 5000 per hour budget. The GraphQL point cost of the list with nested rollups is an estimate. Measuring it is a Wave 1 exit criterion.
- GraphQL has no conditional requests, so ETags do not help. The `updatedAt` and `headRefOid` comparison and `gh api --cache` carry the savings.
- The `x-ratelimit-*` headers, not a hardcoded 5000, drive the "retry at" time.
- Secondary limits are the main risk for writes. Writes are sent one at a time, and 403 or 429 shows the rate-limited state.

#### Feasibility risks

**Wave 1:** the shape and mix of `statusCheckRollup`, the GraphQL point cost of the list, a size cap on `files` and an HTTP 406 on very large diffs (degraded "diff too large" state), and making `markdown()` reusable.

**Wave 2:** logs exist only for Actions checks (run and job ids from the details URL), `--log-failed` fails while a run is in progress, log lines over 64 KiB, `--delete-branch` side effects, whether `autoMergeAllowed` is available from `gh repo view`, and mapping `mergeStateStatus` to failing and pending required checks.

**Wave 3:** the deprecated comment mutation, line mapping through renames and `\ No newline` markers, a pending review started on an older commit, thread and comment node ids versus `databaseId`, and paginating more than 100 threads or 50 comments per thread.

#### Implementation order with exit criteria

Every step adds a test that fails before it and passes after it. Every step ends with `go test -race ./...` and golangci-lint green.

**Wave 1: v0.5, read-only**

1. Runner and error classes. Tests: a missing `gh` gives `Missing`, captured stderr classifies, the command sets the environment, `WaitDelay` and `Dir`. Rules TC-S1, S6, D2.
2. Session branch set in `trail`: per-event branches and live-checkout sampling, kept after a switch. Tests: a Claude session that switches branch keeps both, a Codex session records its start branch, a sampled switch is added, a closed session stops sampling.
3. Fixtures, list decoder and partition. Tests: decode, "mine" from the viewer login, "needs review" from the search result, argument builder. TC-P3.
4. Detail decoder. Tests: both rollup kinds, a deleted author. TC-P3.
5. Open and close the screen. Tests: `p` opens, `esc` closes, global mode sets a notice, the footer shows notices. TC-D1, A7.
6. Staleness. Tests: an older list result and an older generation are dropped. TC-A2, A3.
7. Polling. `pollMsg` carries `at`. Tests: 30 s wait, a closed screen never polls, skip while in flight, `R` fetches once, detail refetched only when changed or pending, startup fetch for the badge. TC-P1, P2, P3, A5.
8. Cancellation. Test: closing the screen cancels the read context. TC-A6.
9. Degraded rows. Test: each `Problem` renders its remedy. TC-D2.
10. Tabs and sanitizing. Tests: control sequences stripped from the body, checks listed. TC-S7.
11. Diff tab. Tests: header-free counts, rename and binary handling, diff styles, too-large state.
12. Sessions link, badge and both jump directions. Tests: badge shows, cross-repo does not link, empty branch does not link, branch history match, hidden-session notice, `p` on a session opens its PR.
13. Help entry for the PR keys.

*Exit:* all tests pass. A manual run in this repo shows the three lists. `gh api rate_limit` before and after ten polls gives the measured cost, recorded in the release notes. Closing the screen leaves no `gh` process running.

**Wave 2: v0.6, writes, logs, re-run**

1. Editor compose. Tests: command resolution order, empty cancels, CRLF normalized, draft kept when the send fails. TC-S8, S9.
2. Comment and reply. Tests: body file used, a second press while pending does nothing, success bumps the generation and refetches. TC-S3, A8, A3, A4.
3. Edit title and body. Test: a title starting with `-` is safe. TC-S2.
4. Resolve and unresolve.
5. Close. Tests: `y` required, an already-closed error refetches. TC-W1, W3.
6. Merge. Tests: only allowed methods, auto-merge when pending, warning on failing checks, `--repo` from the temp directory, result reaches the notice after the screen closes. TC-W2, W5, A7, P5. Manually confirm a local worktree branch survives a merge with branch deletion.
7. One-shot review. Test: body file used.
8. Logs. Tests: run and job from the details URL, a non-Actions check has no logs, last 5000 lines kept, timestamps and ANSI stripped, long lines handled, search highlights. TC-P4, S7.
9. Re-run. Tests: confirm required, arguments use the run id. Manually confirm the scope a real re-run needs.
10. Quit guard. Test: `q` during a write warns first. TC-W7.

*Exit:* all tests pass, plus a manual comment, edit, close, merge, review, log view and re-run on a throwaway PR.

**Wave 3: v0.7, line comments and pending reviews**

1. Confirm the schema and capture GraphQL fixtures. Test: queries use variables, not interpolation. TC-S4.
2. Threads. Tests: pagination decode, resolve then refetch. TC-A4.
3. Line mapping. Tests: table of hunk headers, removed lines refuse, `\ No newline` skipped. TC-W4.
4. Pending review. Tests: an existing pending review is continued, the first comment creates a review at the loaded head, a moved head blocks. TC-W6.
5. Thread and reply arguments.
6. Submit and discard. Tests: each review event, discard asks `y`. TC-W1.

*Exit:* all tests pass, plus a manual round trip of starting a review, commenting on a line, replying, resolving and submitting, with the PR on GitHub matching the TUI.

### Success Metrics

- 100% of requirements R1 to R22 have at least one passing fixture-driven test, and the race detector is clean on every release.
- The Scenario 1 path (session to merged PR) runs end to end in a scripted test against the fake `gh`.
- The measured GraphQL point cost per poll is recorded for Wave 1 and stays within the 360 calls an hour budget.
- Proxies for use, since agent-trail collects no telemetry: release downloads, stars and issues tagged to the PR screen. No targets are set.

### GTM Approach

- **Message.** "Your agent's PR, next to the session that wrote it." Lead with the session link, because gh-dash covers the PR basics.
- **Channels.** Release notes for each wave, a README section and an updated screenshot. No external launch is planned.
- **Staging.** v0.5 is described as read-only. The write waves are announced separately.
- **Prerequisite.** The README states that `gh` must be installed and signed in with `gh auth login`.

### Open Issues

1. Branch matching misses a Codex or Copilot session that switched branch and closed before sampling saw it.
2. Two PRs from one head branch into different bases share a badge (most recent plus `+N`).
3. The GraphQL point cost of the list with nested rollups is unmeasured.
4. A real re-run succeeded with a classic token holding `repo` and `workflow`; the minimum scope was not tested. `gh repo view` does not expose auto-merge, so merge settings come from `gh api repos/{owner}/{repo}` (`allow_auto_merge`), cached for an hour. Those fields appear only for users with push access.
5. Wave 3 is the largest piece. If it slips, Waves 1 and 2 still ship alone.
6. Release dates are not set.
7. Keymap sign-off against the in-app key reference is pending, and `NO_COLOR` handling must be verified when implementing.

### Q&A

| Asked by | Question | Answer |
|---|---|---|
| User | Why shell out to `gh` rather than call the API? | `gh` owns authentication and host configuration, so agent-trail handles no token. |
| User | Does it work across repositories? | No. Current repo only. |
| User | What if `gh` is missing? | An inline message shows on the PR screen. The rest of the app is unaffected. |
| User | Can I merge with failing checks? | Yes, with a warning and `y`. Branch protection still blocks what it must. |
| User | Can I get logs for a non-Actions check? | No. It opens in the browser. |
| User | Can I comment on a range or add a suggestion? | No. Single right-side lines only. |
| User | How does a PR find its sessions? | The PR's head branch must be in the session's branch set, from its events and from sampling its checkout. |
| User | Is it read-only at first? | Yes. v0.5 is read-only. Writes arrive in v0.6 and v0.7. |
| Maintainer | How is it tested without GitHub? | A fake `gh` runner with fixture JSON, a few golden files and the race detector. |

### Feature Timeline and Phasing

| Feature | Status | Dates |
|---|---|---|
| Wave 1: v0.5 read-only (W1-1 to W1-9) | Backlog | Not set |
| Wave 2: v0.6 writes, logs, re-run (W2-1 to W2-8) | Backlog | Not set |
| Wave 3: v0.7 line comments and pending reviews (W3-1, W3-2) | Backlog | Not set |

Sequencing is fixed: Wave 2 starts after Wave 1 ships, and Wave 3 after Wave 2.

### Decision Log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-23 | Same binary, new `p` screen, current repo only | Fits the existing app, avoids a config surface | A separate binary, multi-repo configuration |
| 2026-10-23 | Shell out to `gh`, plus `gh api graphql` for threads, resolve and pending reviews | `gh` owns authentication and transport | A direct GitHub API client |
| 2026-10-23 | List left with three sections, detail right with five tabs | One screen holds the whole review loop | A screen per PR aspect |
| 2026-10-23 | Poll every 30 s only while visible, `R` refreshes, plus one fetch at startup for the session badge | Saves rate limit, and keeps the badge useful | Always-on polling, manual refresh only, no startup fetch |
| 2026-10-23 | Degraded states are an inline message | A missing `gh` must not break session viewing | A blocking startup error |
| 2026-10-23 | Read and write support | The goal is closing the loop | A read-only viewer that opens the browser for each action |
| 2026-10-23 | Edit in `$EDITOR`, and open in browser | Native to the terminal workflow | An in-app text editor |
| 2026-10-23 | Compose all text through a temp file in `$EDITOR` | One mechanism for every write | An inline textarea (may come later) |
| 2026-10-23 | Merge: allowed methods only, delete-branch toggle, auto-merge when pending, warn but allow when failing, `y` confirm | Respects repo policy without taking the decision away | Blocking on failing checks |
| 2026-10-23 | Close needs `y` | It is destructive | No confirmation |
| 2026-10-23 | Review submit plus pending reviews (continue the web one, Pending (N), `S`, discard with confirm) | Supports one-shot and inline review | One-shot only |
| 2026-10-23 | Line comments on single right-side lines | Bounds the size of Wave 3 | Ranges, suggestion blocks |
| 2026-10-23 | Check logs and re-run for Actions checks, browser for others | Actions has uniform log and re-run APIs | Logs for all providers, browser only |
| 2026-10-23 | Diff tab reuses the Files diff styling and layout helpers | Avoids a second set of styles | A new renderer |
| 2026-10-23 | Session link by branch set (events plus live-checkout sampling), including other worktrees, with a PR badge on session rows and two-way jumps | The differentiator against gh-dash | No link, matching by commit SHA, live checkout only |
| 2026-10-23 | Three waves, each its own release (v0.5, v0.6, v0.7) | Value early, isolates the largest piece | One release |
| 2026-10-23 | Deterministic and testable: fake runner, fixtures, substring tests plus at most two golden files, race-clean | Fast suite independent of GitHub | Live GitHub tests, manual testing |
| 2026-10-23 | Capital letters for state-changing keys, `v` for resolve | Predictable, and keeps `x` from changing meaning | Lowercase everywhere |
| 2026-10-23 | `q` during a write warns first | A mid-merge quit leaves the outcome unknown | Wait for the write, quit at once |
| 2026-10-23 | Out of scope: Enterprise Server, PR creation, Windows, telemetry | Not needed for the key persona | Supporting them in v1 |

### Change Log

| Date | Change | Why |
|---|---|---|
| 2026-10-23 | First version | Result of the design interview and the three-way review |

---

## PRD Checklist

| # | Topic | Done |
|---|---|---|
| 1 | Title | Yes |
| 2 | Author | Yes |
| 3 | Decision Log | Yes |
| 4 | Change Log | Yes |
| 5 | Overview | Yes |
| 6 | Success Metrics | Yes |
| 7 | Messaging (GTM Approach) | Yes |
| 8 | Timeline/Release Planning | Yes (dates not set) |
| 9 | Personas | Yes |
| 10 | User Scenarios | Yes |
| 11 | User Stories/Features/Requirements | Yes |
| 12 | Features In | Yes |
| 13 | Features Out | Yes |
| 14 | Design | Yes (mockups pending) |
| 15 | Open Issues | Yes |
| 16 | Q&A | Yes |
| 17 | Other Considerations | Yes (Technical Considerations) |
