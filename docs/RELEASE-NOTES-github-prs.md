# Release notes draft: GitHub pull request screen

## v0.5 (read-only)

- `p` opens the pull requests of the current repository: Needs my review, My PRs and All open, with Overview, Checks, Comments, Diff and Sessions tabs.
- Session rows show the matching `#N`; the Sessions tab jumps to the session, and `p` on a session opens its pull request.
- Requires the GitHub CLI, signed in with `gh auth login`.

Measured rate cost, from `gh api rate_limit` before and after the live argument test (repo, viewer, open list, review-requested list, one threads query): about 2 core and 2 GraphQL points per poll. The search quota was unchanged because `gh` runs `--search` through GraphQL. A steady poll is 2 to 3 `gh` calls every 30 seconds, at most about 360 calls an hour.

## v0.6 (writes)

Reply, comment, edit, resolve, close, merge, one-shot review, Actions check logs and re-run. Every write asks for `y` or opens your editor and is never retried.

## v0.7 (inline review)

Line comments on added or unchanged lines, continuing a pending review, submit and discard.

## Manual verification (2026-10-23)

Run on a throwaway pull request in this repository (scratch head branch into a scratch base branch, so `main` was never touched), using the same argument builders as the app: comment, edit title (a title starting with `-`) and body, edit own comment, line comment that started a pending review at the loaded head, submit it, reply, resolve, unresolve, one-shot review, Actions log (292 lines, timestamps and ANSI stripped), re-run of all jobs, close, reopen, and squash merge with `--delete-branch` run with `--repo` from the temp directory. Every call succeeded. The remote head branch was deleted and the local worktree branch survived the merge.

Findings:
- A workflow re-run succeeded with a classic token holding `repo` and `workflow`. A token with less was not tried, so the minimum scope is still unconfirmed; GitHub's documentation says `repo` is enough on a classic token.
- `gh run rerun --failed` on a run that is still running is refused ("This workflow is already running"); the app shows that stderr text.
- This repository reports `allow_auto_merge: false`, so the auto-merge toggle was not exercised.
- Discarding a pending review was covered by tests only.
