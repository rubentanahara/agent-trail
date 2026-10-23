# Release notes draft: GitHub pull request screen

## v0.5 (read-only)

- `p` opens the pull requests of the current repository: Needs my review, My PRs and All open, with Overview, Checks, Comments, Diff and Sessions tabs.
- Session rows show the matching `#N`; the Sessions tab jumps to the session, and `p` on a session opens its pull request.
- Requires the GitHub CLI, signed in with `gh auth login`.

Measured rate cost, from `gh api rate_limit` before and after the live argument test (repo, viewer, open list, review-requested list, one threads query): about 1.8 GraphQL points per poll, measured over ten polls of the three calls (open list, review-requested list, one detail view) with `gh api rate_limit` before and after (4956 to 4938). The core and search quotas did not move because `gh` runs these through GraphQL. A steady poll is 2 to 3 `gh` calls every 30 seconds, at most about 360 calls an hour.

## v0.6 (writes)

Reply, comment, edit, resolve, close, merge, one-shot review, Actions check logs and re-run. Every write asks for `y` or opens your editor and is never retried.

## v0.7 (inline review)

Line comments on added or unchanged lines, continuing a pending review, submit and discard.

## Manual verification (2026-10-23)

Run on a throwaway pull request in this repository (scratch head branch into a scratch base branch, so `main` was never touched), using the same argument builders as the app: comment, edit title (a title starting with `-`) and body, edit own comment, line comment that started a pending review at the loaded head, submit it, reply, resolve, unresolve, one-shot review, Actions log (292 lines, timestamps and ANSI stripped), re-run of all jobs, close, reopen, and squash merge with `--delete-branch` run with `--repo` from the temp directory. Every call succeeded. The remote head branch was deleted and the local worktree branch survived the merge.

Findings:
- `NO_COLOR=1` removes all colour escapes from the screen; the normal run emits them.
- A workflow re-run succeeded with a classic token holding `repo` and `workflow`. A token with less was not tried, so the minimum scope is still unconfirmed; GitHub's documentation says `repo` is enough on a classic token.
- `gh run rerun --failed` on a run that is still running is refused ("This workflow is already running"); the app shows that stderr text.
- This repository reports `allow_auto_merge: false`, so the auto-merge toggle was not exercised.
- Discarding a pending review was run live on a second throwaway pull request: a line comment started a pending review and `deletePullRequestReview` removed it.
