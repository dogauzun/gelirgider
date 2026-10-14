# ADR 0015: PR review by a cloud routine, no GitHub Actions

Status: accepted (GLG-580, 2026-09-28); amended 2026-09-28 (GLG-582, a
two-line review body and two-sentence inline findings, no notes); amended
2026-09-29 (GLG-590, see "Amendment 2026-09-29"); superseded in part by
[ADR 0022](0022-pr-review-agent.md) (GLG-762, 2026-10-02): the review
routine and its verdict are gone, the no-Actions decision stands; amended
2026-10-07 (GLG-1115, personal disclosure removed); amended 2026-10-14
(GLG-1373, the no-Actions decision no longer stands: the required GitHub
Actions checks are the merge bar)

## Context

The repository had two GitHub Actions workflows left: `claude.yml`
(answers `@claude` mentions) and `claude-code-review.yml` (reviews every
PR with the code-review plugin). Neither has run since 2026-09-23. The
CI jobs for backend and frontend were already removed (GLG-257, GLG-305);
every check runs locally before merge.

Development is done by AI agents: an hourly cloud routine implements
tickets through one subagent per ticket, reviews the diff itself and
merges. A second, independent reader of every diff is worth having, and
it has to work without Actions.

## Decision

### Workflows deleted, no Actions

`.github/workflows/claude.yml` and `.github/workflows/claude-code-review.yml`
are deleted and `.github/` with them. Nothing runs on GitHub. Backend and
frontend checks stay local, run by whoever merges, as CLAUDE.md says.

> Superseded 2026-10-14 (GLG-1373): the repository is public since 2026-10-14 and `.github/workflows/checks.yml` runs `make lint`, `make test` and `make build-frontend` on every pull request and on `main` (GLG-1372), with `.github/workflows/pr-title.yml` checking the PR title (GLG-1374). The no-Actions decision no longer stands: the four checks are required on `main`, and green on the PR head they are the merge bar in place of the merger's local run (GLG-1373).

### `pr review` cloud routine

A cloud agent named `pr review` is fired by a
GitHub webhook on `pull_request` events `opened`, `synchronize`,
`reopened` and `ready_for_review`, and once an hour by a schedule that
walks every open PR. Each run reviews the PR's current head commit
against CLAUDE.md, `docs/PRINCIPLES.md`, the relevant ADR and the GLG
ticket, and posts a GitHub review for that commit:

- inline comments anchored to diff lines, one per finding, at most ten,
  two sentences each: what is wrong and the fix;
- a body of exactly two lines, `Verdict: APPROVE` or
  `Verdict: REQUEST_CHANGES`, then `Head: <sha>`. A finding with no diff
  line to anchor to (missing ticket key, attribution in a commit, PR body
  off the template) follows as one line each. No summary, no notes: an
  observation that does not block the merge is not posted.

The routine never edits code, never pushes, never merges and never writes
to Jira. It does not run the check suite; the diff is its job, the build
is the merger's. It skips drafts, a head whose newest review says
`APPROVE`, and a head whose `REQUEST_CHANGES` review still has an open
finding. A head whose findings were all fixed or answered and resolved
is reviewed again and gets a fresh verdict. A run started by an event
whose head SHA is no longer the PR's head exits without posting.

> Superseded 2026-10-02 by [ADR 0022](0022-pr-review-agent.md) (GLG-762): the routine and its webhook are disabled. The merger spawns a review subagent that posts inline comments only, with no review body.

### Verdict in the body, review state `COMMENT`

Every PR is authored by the repository owner and the routine runs as the
same account. GitHub refuses `APPROVE` and `REQUEST_CHANGES` reviews on
the author's own PR, so the review is posted with state `COMMENT` and the
verdict is carried by the first line of the body. The `Verdict:` and
`Head:` lines are the contract other automation parses.

> Superseded 2026-10-02 by [ADR 0022](0022-pr-review-agent.md) (GLG-762): reviews still have state `COMMENT`, but no verdict is posted or parsed.

### Merge gate

A PR merges when all of these hold (CLAUDE.md rules 6 and 7):

1. the newest routine review whose `Head:` line is the current head SHA
   says `Verdict: APPROVE` (older reviews for that head are superseded);
2. every local check passed on that head, observed by the merger;
3. every review thread, routine or human, is fixed or answered and
   resolved; out-of-scope findings became GLG tickets.

The merger waits up to 30 minutes after a push for the review on that
SHA. On timeout it may merge and must say so in its merge comment. A
`REQUEST_CHANGES` verdict sends the findings back to the ticket's agent;
the push that fixes them triggers a new review on the new SHA. A finding
answered without a push (a disagreement with evidence, or a problem
handed to its own GLG ticket) is replied to and resolved; the merger then
switches the PR to draft and back to ready, which fires a
`ready_for_review` review of the same head, and waits up to 30 minutes
for it. After three `REQUEST_CHANGES` rounds on one PR the merger parks
the ticket with the `needs-decision` label instead of a fourth round.

> Superseded 2026-10-02 by [ADR 0022](0022-pr-review-agent.md) (GLG-762): the gate is a review of the PR's current diff by the review subagent, resolved threads and green local checks. No wait, no timeout merge, no draft and ready toggle; the round cap is six instead of three.

## Consequences

- Every PR gets an independent second reader within minutes of a push,
  without Actions minutes or a bot subscription.
- The hourly kanban worker keeps its own diff read and its local checks,
  and adds the wait for the routine's verdict before merging.
- Review quality depends on the routine prompt. Rule changes in CLAUDE.md
  or PRINCIPLES.md must be mirrored there.
- There is still no CI: a PR whose author skips the local checks is
  caught only by the merger running them.

> Superseded 2026-10-02 by [ADR 0022](0022-pr-review-agent.md) (GLG-762): the first three consequences describe the routine and no longer hold; the last one does.

> Superseded 2026-10-14 (GLG-1373): the last consequence no longer holds either. GitHub Actions runs the checks on every pull request (GLG-1372, GLG-1374).

## Amendment 2026-09-29 (GLG-590)

Two PRs (#544, #546) stalled on 2026-09-28: a `REQUEST_CHANGES` review
with no finding on GitHub, a behavior-neutral one-character diff flagged
as an unrelated change that the worker could not undo through the GitHub
contents API, two runs racing on one head with opposite verdicts, and no
way to get a fresh verdict after answering a finding without a push. The
contract above now includes:

- The newest review for a head decides. A `REQUEST_CHANGES` review always
  carries a visible finding: an inline comment, a body line, or a reply in
  an existing thread. A review that went out without its findings is
  followed by a corrected review for the same head.
- Not findings: a difference that leaves runtime behavior identical (an
  escape sequence versus the character it denotes, whitespace, quote
  style, import order, formatting prettier accepts), and a problem the PR
  body or a thread hands to a named GLG ticket (CLAUDE.md rule 4).
- The cloud sandbox has no `gh` CLI and no GraphQL. Both routines use the
  GitHub MCP tools for reviews, comments and merges, and git over HTTPS for
  everything that touches a branch. A PR branch changes only through git
  under the owner's identity, never through the contents API or the
  update-branch endpoint, which decode escapes and add merge commits.
- Merges pass an explicit commit title and message; the merge tool adds a
  `Co-authored-by` trailer otherwise (rule 9).

> Superseded 2026-10-02 by [ADR 0022](0022-pr-review-agent.md) (GLG-762): the first two points describe the routine's verdict and findings and no longer apply. The last two, on the cloud sandbox tools and on merge commit messages, still hold.

## Alternatives rejected

| Option                                  | Why not                                                                                                                                      |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| Keep `claude-code-review.yml`           | Actions cost and an account dependency for a job one cloud routine replaces; the mention workflow had no user.                               |
| Cron polling every 15 minutes           | Up to 96 idle runs a day and 15 minutes of latency; the webhook fires only on a push. Kept as the fallback if webhook triggers stop working. |
| Keep the Cursor app as reviewer         | Its approvals were a second opaque bot with no access to the ticket or the ADRs.                                                             |
| Routine as the only reviewer and merger | Puts one more moving part on the critical path; the worker already reads the diff and runs the checks, the routine adds an independent read. |
