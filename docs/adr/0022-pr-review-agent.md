# ADR 0022: PR review agent, its findings and its exempt diffs

Status: accepted (2026-10-02).

## Context

Development is done by AI agents. A second, independent reader of every
diff is worth having, but it must not become a second moving part with its
own failure modes. A review routine fired by a webhook and an hourly
schedule had them: a verdict posted without its findings, two runs racing on
one head, a wait that ended in a timeout merge, and a draft and ready toggle
to get a fresh verdict. Its checklist also covered what the linters, the
tests and the merger already cover, so the few findings that mattered sat
among findings about scope wording, hygiene and style. [ADR 0015](0015-pr-review-routine.md)
records that routine.

## Decision

### A separate review agent

Every PR, except one whose diff is exempt (below), is reviewed by a separate
review agent, `pr-reviewer`, on Opus 5.5 at high effort. How the review runs
is in [ADR 0037](0037-pr-review-workflow.md): the `Claude Code Review`
workflow on every push (a required check), and the merger's subagent for a
fork PR, a Dependabot PR, a PR that changes the workflow file, and any PR
while the owner has lifted the required check for an outage. It runs
alongside the other required GitHub Actions checks, not after them: a lint
failure rarely changes a finding, and waiting for the checks added their run
time to the review time of every PR. The checks stay a condition for the
merge. Every push that changes the PR's diff and leaves it non-exempt gets a
fresh review, which includes a push that fixes a failing check. The agent that
wrote the code never reviews it, and the reviewer starts without the author's
context.

### Exempt diffs

The reviewer posts findings on about 15 to 20% of PRs, and a docs-only review
once took over an hour. A PR whose diff touches only exempt paths skips the
reviewer; the required checks and the merger's hygiene step suffice. The list
is closed and decided by the file paths of the diff, never by the PR title
type. It lives only in the `scope` step of
`.github/workflows/claude-code-review.yml`, which runs the match on every push
and ends the job green with a line in the job summary for an exempt diff
(ADR 0037); no other document restates it, and a change to it is a change to
that step. The list never gains a `Makefile`, a dependency manifest or a
lockfile.

The list covers the paths where a second reader finds little: documentation
and root `*.md` files that are not a rule document, `.github/`, `deploy/`,
the UI strings under `frontend/src/i18n/`, and a few plain chore files. Everything
else is reviewed: the rule documents (`CLAUDE.md`, `docs/PRINCIPLES.md`,
`docs/adr/`), because the merger's hygiene step does not compare documents
with each other and the reviewer's findings on them have been real
inconsistencies; `.claude/`, because agent files and skills steer
agents; the `Makefile`, dependency manifests and lockfiles; `backend/`;
and `frontend/` outside `frontend/src/i18n/`. A diff that touches one path
outside the list is reviewed in full.

The author is the `ticket-implementer` agent, on Sonnet 5.5 at high effort.

Both agents' instructions live in the repository, in
`.claude/agents/pr-reviewer.md` and `.claude/agents/ticket-implementer.md`,
so every session uses the same text. A session whose
harness does not load those files as agent types spawns a general subagent
with the same model and effort and tells it to read the file and follow it.

No session parses a verdict. The review of a head ends with its comments.

### What the reviewer reports

- Inline comments, one per finding, on a line the PR added or changed. No
  review body, no summary, no verdict. A clean PR gets nothing posted.
- Only high-value findings: a potential bug, a security issue, or an
  inconsistency (with the ticket, an ADR, a project rule that has a real
  consequence, or within the change itself). Each one names a concrete
  failure.
- Only the change. Code the PR does not touch is out of scope, even when
  it is wrong; the reviewer reads it only to judge the change.
- Not a linter: nothing that `make lint`, the tests or the build report,
  no style, no naming, no suggestions, no questions, no praise.

The reviewer never edits code, pushes, merges, resolves a thread or
writes to the tracker. It ends with a short report: the head SHA it reviewed and
either `no findings` or one line per comment posted. Only a run as the
merger's subagent (a fork PR, a Dependabot PR, a PR that changes the
workflow file, or any PR while the owner has lifted the required check for an
outage) delivers it to anyone.

A subagent review is posted with state `COMMENT`, because GitHub refuses
`APPROVE` and `REQUEST_CHANGES` on the author's own PR and every PR of the
repository is authored by the account the agents run as. The workflow
posts inline comments only and no review.

### What the merger does

The merger does not read the whole diff. It:

1. reads the `Claude Code Review` run on the current head (ADR 0037). It
   concludes `success` either after a review or, for an exempt diff, without
   one; the job summary says which. The merger matches no paths itself;
2. reads the required GitHub Actions checks on the PR head, once they have
   run: `Go lint`,
   `Web lint`, `Go tests`, `Web tests` and `Web build` (workflow `ci`), `Title`
   (workflow `pr`) and `Claude review` (workflow `Claude Code Review`), see
   `docs/PRINCIPLES.md` #6;
3. checks hygiene: no AI attribution in commits or the PR body, the PR body
   says what changed and how it was verified, non-trivial logic
   ships with a test, a bug fix's PR body names a test that failed
   before the fix or says why no test can express the bug, and a new or
   changed route has its swag annotation (the missing tests and documentation
   the reviewer leaves to the merger);
4. reads the review's result (an exempt PR has none);
5. sends findings that need a code change back to the ticket's agent, and
   answers the others in their thread: a disagreement with evidence, or a
   new GLG ticket for a real but out-of-scope problem (CLAUDE.md rule 4);
6. resolves every thread once it is fixed or answered.

The merger posts a merge comment on the PR that lists the required checks with
their run URLs and conclusions on the merged head, and the review run's URL and
conclusion on that head (with `exempt diff, not reviewed` when its job summary
says so), and it transitions the ticket to `Done`
with a comment that links the merged PR.

### Merge gate

A PR merges when all of these hold (CLAUDE.md rules 6 and 7):

1. the PR's current head has been reviewed or is exempt, as ADR 0037 states
   (the run's job summary says which); a push that adds a non-exempt path
   ends the exemption, and the run on that head reviews. A diff that touches
   `.github/workflows/claude-code-review.yml` is never satisfied by an exempt
   run: the merger's subagent reviews it;
2. every review thread, from the reviewer or a human, is fixed or
   answered and resolved;
3. the required checks are green on the current head. The merger turns on
   auto-merge and GitHub merges as soon as they pass; the branch does not
   have to be up to date with `main`, so no rebase is needed first. A push
   run on `main` that fails after a merge is a bug ticket for the merger
   that landed it. The procedure is in [CLAUDE.md](../../CLAUDE.md) rule 7.

A push that changes the PR's own diff gets a new review, unless the diff is
still exempt. A finding that was answered without a push does not.

A PR branch changes only through git under the owner's identity, never
through the GitHub contents API or the update-branch endpoint, which decode
escape sequences and add merge commits; the one exception is `gh pr update-branch`
for a head that predates a required-check change (CLAUDE.md rule 7). A merge passes an explicit commit
title and message, because the merge tool adds a `Co-authored-by` trailer
otherwise (CLAUDE.md rule 9) and the repository's squash default would
land a single-commit PR under the commit's subject, copy branch commit
trailers into `main` and add a `Co-authored-by` line for each branch commit
author other than the merger.

A round is one review that posted findings; after six rounds on one PR
the merger parks the ticket with the `needs-decision` label instead of a
seventh.

## Recorded simplifications

- A review that starts while the checks run is wasted when a failing check
  forces a push that changes the diff. The cost is accepted.
- The reviewer and the author run on the same model family. The
  independence comes from a fresh context and a different brief, not from
  a different model.
- In a cloud session (a subagent review) the
  GitHub proxy appends a `Generated by Claude Code` footer to every review comment and
  re-appends it on every edit. The owner decided that reviews and review
  comments stay as posted, footer included; it is not a CLAUDE.md rule 9
  breach to report or fix.

## Consequences

- One reader per non-exempt diff. Review quality depends on
  `.claude/agents/pr-reviewer.md`; a change to what counts as a finding is
  made there, in a PR.
- Fewer comments. Problems in old code and anything a linter would say
  never reach a PR thread.

## Alternatives rejected

| Option                                         | Why not                                                                                                        |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| A cloud routine fired by a webhook and a timer | The webhook, the polling, the verdict parsing and the race between runs, for no gain over a subagent.          |
| A one-line "reviewed" marker comment per head  | One more comment on every PR to carry information the merger already has from the reviewer's report.           |
| Merger keeps its own full diff read            | Two reads of the same diff by the same model family; the merger's time goes to the checks, hygiene and triage. |
