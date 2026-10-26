# Orchestration

For a session that runs one implementer per task. `CLAUDE.md` rules 12 and 13 hold the base rules; this file adds what they leave out.

## Stalls and denials

- A subagent that waits (`Monitor`, `gh pr checks --watch`, a background watcher) ends its turn and stalls silently. Implementers open the PR and return; the merger reads the review run. A stalled agent resumes with a `SendMessage` telling it to continue.
- The auto-mode permission classifier sometimes denies `gh pr merge`, wrapper scripts, and `git rebase` plus `push --force-with-lease`, in subagents and in the orchestrator. Retry or split the command; merging `origin/main` into the branch plus a normal push is allowed. When an agent reports a denial, the orchestrator does the step itself; if it is denied there too, stop and tell the owner which steps are pending.
- A classifier outage (no verdict for many calls in a row ends the turn) is waited out with a scheduled wake-up, not retries.

## Shared state

- Sibling subagents share the orchestrator's scratchpad directory: scratch files start with the task's ID.
- `git fetch` from several worktrees at once can fail with "cannot lock ref"; a retry works.
- Auto-merge absorbs a moved `main`. Setting a wave's tasks to `In Progress` before dispatch is the lock against a parallel merger.
- A cloud session started from a local worktree session uses that branch, so the branch must be on GitHub first.

## Waves

- Parallel tasks that touch the same file can merge cleanly and still break `main` together. After a wave lands, read the push run on `main`; a red `main` is a bug task for the merger that landed it.
- Plan a wave by hot file: one task per hot file at a time, everything else in parallel, within the cap of about five implementers. Tell parallel agents to insert into shared registries next to existing entries, not at the end of the file.
- Isolated subagents rediscover the same out-of-scope bug and each files a task. The orchestrator compares the names of new out-of-scope tasks and closes duplicates ([tracker.md](tracker.md)).
- An agent can silently absorb a neighbouring task's scope when that is the root-cause fix for its own bug. Re-read `main` before launching a dependent task and narrow the agent's prompt to what is left. A task goes stale after a few dozen commits: groom it right before dispatch.
- A task that only plugs into a page another open task is building runs in two phases. This is the exception that `CLAUDE.md` rules 12 and 13 name. Phase A builds the independent parts (lib, chart, card, tests, i18n) and commits locally without pushing. When the blocker merges, the message "start phase B" triggers the rest (merge `main`, mount, browser check, PR). Start a phase-A agent for every task whose blocker only affects where its code mounts.

## Briefs and reviews

- Read a task's comments, not only its body, before correcting an agent: owner decisions live in comments.
- Give a `pr-reviewer` subagent the task page URL. Reviewers use `git show` / `git diff` on refs or their own worktree, never `git checkout` in the main checkout.
- A reviewer brief that names the invariant finds the real bugs. A fix moved into a shared function needs "check every caller". After a first finding, ask the agent to sweep every action of the same shape before pushing.
- An attribution grep for "claude" trips on `CLAUDE.md`; match `co-authored|generated with`.

## Checks

- A test that fails only under load may be a real race: reproduce it in a loop before touching it.
- A schema file that rebuilds a table also migrates the owner's dev database on the next start; say so in the final report.
- Browser check width: compare `document.documentElement.scrollWidth` with the emulated width, never `innerWidth`. A fresh temporary DB needs its sources disabled via `PUT /api/admin/sources/{name}` before seeding ([qa-run](../skills/qa-run/SKILL.md)).

## Housekeeping

- `make setup` installs the frontend dependencies in a worktree.
- `make prune-worktrees` removes finished worktrees; run it with `DRY=1` first. It keeps locked, dirty, detached, current, just-cut, unpushed, unverifiable and data-holding worktrees, and leaves other sessions' worktrees alone.
