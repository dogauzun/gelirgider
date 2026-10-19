# ADR 0037: PR review by a GitHub Actions workflow

Status: accepted (2026-10-19).

## Context

[ADR 0022](0022-pr-review-agent.md) made the merger spawn the `pr-reviewer`
agent on every PR and again after every push that changed the diff. That put
the "has the current head been reviewed" bookkeeping in the merger, and a
merger that died or skipped it left a PR unreviewed. The repository has been
public since 2026-10-14, so Actions minutes are free. A review workflow was
tried before, in the private repository, with the generic `code-review`
plugin prompt ([ADR 0015](0015-pr-review-routine.md) and ADR 0022's Context
record the earlier review generations); this time the prompt is the
repository's own agent file.

## Decision

### A workflow runs the review

`.github/workflows/claude-code-review.yml`, workflow `Claude Code Review`, job
`Claude review`, runs `anthropics/claude-code-action` on every pull request event
`opened`, `synchronize`, `ready_for_review` and `reopened`. Its prompt tells
Claude Code to read `.claude/agents/pr-reviewer.md` and review the PR by it,
so that file stays the single source of the review rules: what is a finding,
what is not, where comments go. The model is `claude-opus-5-5` at high effort.

- It fires on every push by itself. A concurrency group per PR number
  cancels the run for the old head when the head moves, so no session
  respawns anything.
- The agent posts each finding as an inline comment through the action's
  inline comment tool, and nothing else: the action's progress comment and
  sticky comment stay off, there is no review body and no verdict. A clean PR
  gets nothing posted; the run's conclusion is the record that the head was
  reviewed.
- The job runs only for a PR whose head is in this repository and whose
  actor is not `dependabot[bot]`. A run for a fork has no secrets, nor has one
  for Dependabot, and the trigger is `pull_request`, never
  `pull_request_target`, which would run fork code with them. A skipped job
  counts as passing for a required check.
- The workflow runs on every push and has no `paths` filter: a required check
  that never starts would hold the PR forever (the same reason `checks.yml`
  has none), and a head with no run would leave the merger to guess whether
  the run is late or the diff is exempt. Its `scope` step reads
  `git diff --name-only origin/main...HEAD`. When every path is exempt
  (ADR 0022) it writes `exempt diff (ADR 0022), not reviewed` to the job
  summary and ends the job `success` without calling the action. The step is
  the one home of the exempt list: no document restates it, and a change to it
  is a change to that step.
- The run authenticates with the `CLAUDE_CODE_OAUTH_TOKEN` repository secret
  (the owner's Claude subscription) and writes to the PR through the Claude
  GitHub App, which the owner installs. The job's own `GITHUB_TOKEN` is
  read-only.

### Repository settings

The Actions policy was `github_owned_allowed`. It now also allows two
patterns: `anthropics/claude-code-action@*` and `oven-sh/setup-bun@*`, the
one action the review action runs as a nested step (pinned to a SHA inside
it, so the pattern is the only form that survives its bumps). Nothing else
third-party is allowed, and every other workflow keeps using GitHub-owned
actions only. The owner holds the settings: the policy, the app install and
the secret.

### A required check

The job `Claude review` is a required status check on `main`
(`docs/PRINCIPLES.md` #6), added 2026-10-19 on a security audit after the
repository went public. The four CI checks go green in about four minutes, so
a merger that turned on auto-merge before the review finished landed a PR
before any finding was posted. With the check required, and "conversations
must be resolved" on, GitHub holds a PR until the review has finished on its
current head and every comment it posted is resolved. This reverses the
first version of this ADR, which left the check optional so that an outage
would not block merges.

- A run that ends `failure` (an API outage, an expired token) is re-run until
  it succeeds. A lasting outage goes to the owner: the ticket gets the label
  `needs-decision`.
- The escape hatch is the owner's. For the outage the owner removes the
  context `Claude review` from the branch protection of `main`, and restores
  it afterwards. `enforce_admins` is on, so nobody bypasses the check. While it
  is off, the merger reviews each head with a subagent, as for a fork PR (the
  merge gate below).

### A fork PR, a Dependabot PR, and a PR that changes the workflow file

The workflow skips a fork PR and a Dependabot PR, and the skipped job passes
the required check. The action also skips itself, with a warning and a run
that still concludes `success`, when the PR's copy of `claude-code-review.yml`
differs from the one on `main` (its app-token exchange validates the file), so
a PR that changes the workflow file gets no review from the workflow either,
whatever else it touches. In all three cases the check is green without a
review, and the merger spawns `pr-reviewer` as a subagent, as ADR 0022
described for every PR, with the same agent file; the agent file says how it
behaves in each place. The merger reads the changed file list for these three
cases only, so it knows the second before it reads the run.

### What the merger does now

- It spawns a reviewer only for a fork PR, a Dependabot PR, a PR that
  changes the workflow file, and any head while the owner has lifted the
  required check for an outage; it never respawns one after a push for a PR
  the workflow reviews.
- It reads the workflow run for the PR's current head, with
  `gh run list --commit <head SHA>` (in a cloud session the GitHub MCP
  `list_workflow_runs`) on `claude-code-review.yml`, and waits while the run is
  queued or in progress. Every head has a run: an exempt diff ends `success`
  within a minute without a review, and the job summary says
  `exempt diff (ADR 0022), not reviewed`. The merger matches no paths itself.
  A head with no run after a few minutes means the run is broken, not that the
  diff is exempt.
- The hygiene step, the thread triage, the six-round limit and the merge
  comment of ADR 0022 stay. The merge comment names the review run's URL and
  conclusion instead of a reviewer head, and says `exempt diff, not reviewed`
  when the job summary does.

### Merge gate

ADR 0022's gate item 1 becomes: `Claude review` concluded `success` on the
PR's current head and the diff does not touch
`.github/workflows/claude-code-review.yml`; or the PR is from a fork, from
Dependabot, or changes the workflow file, or the owner has lifted the required
check for an outage, and the merger's subagent reviewed the current head.
`success` means reviewed or exempt, and the job summary says which; the
conclusion alone never shows a review. GitHub enforces the wait: the check is required, so
it is not green until the run concludes. A push of any
kind starts a new run, so the merger reads the run on the head it is about to
merge. Items 2 and 3 are unchanged: every thread fixed or answered and
resolved, then auto-merge once the required checks are green. When the merger
turns auto-merge on and off is in [CLAUDE.md](../../CLAUDE.md) rule 7.

## Consequences

- No merger session carries respawn logic, and a PR opened by hand gets
  reviewed like any other.
- The review depends on Anthropic's API, one OAuth token and the Claude
  GitHub App. Any of them failing holds every non-exempt PR until a re-run
  succeeds or the owner lifts the requirement (above).
- One third-party action is trusted with a secret and a write token. It is
  pinned to a commit SHA that Dependabot bumps, and it pulls
  `oven-sh/setup-bun` at a SHA of their choosing.
- A change to `.github/` alone is exempt: the job ends green after its first
  step, with a summary line, without calling the action.

## Alternatives rejected

| Option                                          | Why not                                                                                      |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Keep the merger spawning the reviewer           | The merger carries the respawn after each push, and a lost session leaves the PR unreviewed. |
| The generic `code-review` plugin prompt         | A second, unreviewed copy of the review rules; the agent file is the single source.          |
| Keep `Claude Code Review` optional              | Only a process rule held the merge; a merger that turned on auto-merge early skipped it.     |
| `pull_request_target` so forks get a review too | It runs with the repository's secrets on code the fork controls.                             |
