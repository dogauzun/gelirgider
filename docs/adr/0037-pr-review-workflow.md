# ADR 0037: PR review by a GitHub Actions workflow

Status: accepted (2026-10-19).

## Context

[ADR 0022](0022-pr-review-agent.md) made the merger spawn the `pr-reviewer`
agent on every PR and again after every push that changed the diff. That put
the "has the current head been reviewed" bookkeeping in the merger, and a
merger that died or skipped it left a PR unreviewed. The repository has been
public since 2026-10-14, so Actions minutes are free. A review workflow was
tried before, in the private repository, with the generic `code-review`
plugin prompt ([ADR 0015](0015-pr-review-routine.md) records why that
generation was dropped); this time the prompt is the repository's own agent
file.

## Decision

### A workflow runs the review

`.github/workflows/claude-code-review.yml`, workflow `Claude Code Review`, job
`review`, runs `anthropics/claude-code-action` on every pull request event
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
- The job runs only for a PR whose head is in this repository. A run for a
  fork has no secrets, and the trigger is `pull_request`, never
  `pull_request_target`, which would run fork code with them.
- The exempt list of ADR 0022 is reproduced in the workflow's path filter, so
  a diff that touches only exempt paths starts no run. GitHub's `paths-ignore`
  cannot hold `!` patterns, so the filter is `paths` with `**` and the
  exempt paths excluded. The list stays defined in ADR 0022; a change to it
  changes the filter in the same PR.
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

### Not a required check

`Claude Code Review` is not among the required checks on `main`
(`docs/PRINCIPLES.md` #6). An API outage or an expired token must not block
merges. The merger enforces the review in the merge gate below, and a run
that ends `failure` is re-run once; when it fails again the merger falls back
to spawning the reviewer as a subagent, as for a fork PR.

### A fork PR, and a PR that changes the workflow file

The workflow skips a fork PR. The action also skips itself, with a warning
and a run that still concludes `success`, when the PR's copy of
`claude-code-review.yml` differs from the one on `main` (its app-token
exchange validates the file), so a PR that changes the workflow file gets no
review from the workflow either, whatever else it touches. In both cases the
merger spawns `pr-reviewer` as a subagent, as ADR 0022 described for every
PR, with the same agent file; the agent file says how it behaves in each
place. The merger reads the changed file list, so it knows the second case
before it reads the run.

### What the merger does now

- It no longer spawns a reviewer on a PR of the repository itself, and no
  longer respawns one after a push.
- It decides from the changed file list (`gh pr diff <n> --name-only`) whether
  the diff is exempt, as before. For a non-exempt diff it reads the workflow
  run for the PR's current head, with `gh run list --commit <head SHA>`
  (in a cloud session the GitHub MCP `list_workflow_runs`) on
  `claude-code-review.yml`, and waits while the run is queued or in progress.
- The hygiene step, the thread triage, the six-round limit and the merge
  comment of ADR 0022 stay. The merge comment names the review run's URL and
  conclusion instead of a reviewer head, or says `reviewer skipped (exempt
paths)`.

### Merge gate

ADR 0022's gate item 1 becomes: `Claude Code Review` concluded `success` on
the PR's current head and the diff does not touch
`.github/workflows/claude-code-review.yml`; or the diff is exempt and no run
exists; or the PR is from a fork, or changes the workflow file, or the run
failed twice, and the merger's subagent reviewed the current head. A push of any kind starts a new run, so the merger waits for the run on the head it is about to merge. Items 2 and 3 are unchanged: every thread fixed or
answered and resolved, then auto-merge once the required checks are green.
The merger turns auto-merge on only after the run on the current head has
concluded and every thread is resolved, and runs `gh pr merge --disable-auto`
before a push that changes the diff.

## Consequences

- No merger session carries respawn logic, and a PR opened by hand gets
  reviewed like any other.
- The review depends on Anthropic's API, one OAuth token and the Claude
  GitHub App. Any of them failing leaves a PR to the fallback above.
- One third-party action is trusted with a secret and a write token. It is
  pinned to the `v1` tag, which the action's maintainers move, and it pulls
  `oven-sh/setup-bun` at a SHA of their choosing.
- A change to the workflow or to `.github/` alone is exempt, so the workflow
  is first exercised by the next PR that touches non-exempt paths.

## Alternatives rejected

| Option                                          | Why not                                                                                      |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Keep the merger spawning the reviewer           | The merger carries the respawn after each push, and a lost session leaves the PR unreviewed. |
| The generic `code-review` plugin prompt         | A second, unreviewed copy of the review rules; the agent file is the single source.          |
| Make `Claude Code Review` a required check      | An API outage or an expired token would block every merge.                                   |
| `pull_request_target` so forks get a review too | It runs with the repository's secrets on code the fork controls.                             |
