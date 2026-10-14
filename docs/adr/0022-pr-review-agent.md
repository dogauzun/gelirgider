# ADR 0022: PR review by a subagent of the merger

Status: accepted (GLG-762, 2026-10-02); amended 2026-10-05 (GLG-974, the
merger's hygiene step checks tests and swag annotations); amended 2026-10-14
(GLG-1373, the required GitHub Actions checks replace the merger's local
check run)

## Context

[ADR 0015](0015-pr-review-routine.md) gave every PR a review from the
`pr review` cloud routine: a GitHub webhook and an hourly schedule started
a separate cloud session, which posted a review whose body carried
`Verdict: APPROVE` or `Verdict: REQUEST_CHANGES` and `Head: <sha>`. The
merger polled for that body for up to 30 minutes after every push, and
read the whole diff itself as well.

The routine was a second moving part with its own failure modes: a
verdict posted without its findings, two runs racing on one head, a wait
that ended in a timeout merge, and a draft and ready toggle to get a fresh
verdict (the 2026-09-29 amendment of ADR 0015). Its checklist also covered
what the linters, the tests and the merger already cover, so reviews
carried findings about scope wording, hygiene and style next to the few
that mattered.

The owner decided on 2026-10-02 to drop the routine and to narrow what a
review is for.

## Decision

### A review subagent, spawned by the merger

Every PR is reviewed by a separate review agent on Opus 5.5. The merger
spawns it as a subagent: the hourly kanban worker in the cloud, the
orchestrating session locally. It runs once the PR is open and the local
checks are green, and a fresh one runs after every push that changes
the PR's diff. The agent that wrote the code never reviews it, and the
reviewer starts without the author's context.

The reviewer's instructions live in the repository, in
`.claude/agents/pr-reviewer.md`, so the cloud worker and a local session
use the same text. A session whose harness does not load that file as an
agent type spawns a general subagent on the same model and tells it to
read the file and follow it.

The `pr review` routine and its webhook trigger are disabled. No session
waits for or parses a `Verdict:` body any more.

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
writes to Jira. It ends with a short report to the merger: the head SHA
it reviewed and either `no findings` or one line per comment posted.

The review is posted with state `COMMENT`, because GitHub refuses
`APPROVE` and `REQUEST_CHANGES` on the author's own PR and every PR is
authored by the account the agents run as (unchanged from ADR 0015).

### What the merger does

The merger no longer reads the whole diff. It:

1. runs every local check on the PR head (`docs/PRINCIPLES.md` #6);
2. checks hygiene: no AI attribution in commits or the PR body, the PR
   body follows the template, the branch contains current `main`;
3. spawns the reviewer and reads its report;
4. sends findings that need a code change back to the ticket's agent, and
   answers the others in their thread: a disagreement with evidence, or a
   new GLG ticket for a real but out-of-scope problem (CLAUDE.md rule 4);
5. resolves every thread once it is fixed or answered.

> Amended 2026-10-05 (GLG-974): step 2 also checks that non-trivial logic
> ships with a test, that a bug fix's Testing section names a test that
> failed before the fix or says why no test can express the bug (the GLG-959
> rule), and that a new or changed route has its swag annotation. These are
> the missing tests and documentation that `.claude/agents/pr-reviewer.md`
> leaves to the merger.

> Superseded 2026-10-14 (GLG-1373): step 1 is reading the required GitHub Actions checks (`lint`, `test`, `build-frontend`, `pr-title`) on the PR head, see [ADR 0015](0015-pr-review-routine.md); the merger no longer runs them locally, and the reviewer is spawned once they are green.

### Merge gate

A PR merges when all of these hold (CLAUDE.md rules 6 and 7):

1. the reviewer has reviewed the PR's current diff: the current head, or
   the head before a push that only rebased the branch onto `main` and
   resolved no conflict;
2. every review thread, from the reviewer or a human, is fixed or
   answered and resolved;
3. every local check passed on the current head, the commit that
   merges, observed by the merger. A rebase needs the checks again even
   when it needs no new review.

A push that changes the PR's own diff gets a new review. A push that only
rebases the branch onto `main` does not, and neither does a finding that
was answered without a push. A rebase that had to resolve a conflict
rewrites the PR's own lines, so it counts as a change and gets a review.

> Superseded 2026-10-14 (GLG-1373): item 3 is the required checks green on the current head. GitHub runs them again on every push, and `main` requires the branch to be up to date.

A round is one review that posted findings; after six rounds on one PR
the merger parks the ticket with the `needs-decision` label instead of a
seventh.

## Recorded simplifications

- Nothing on GitHub says that a clean PR was reviewed. The merger's merge
  comment states that the reviewer ran and on which head.
- A PR opened outside a merger session (by hand, or by an agent whose
  orchestrator died) gets no review until a merger picks it up. The hourly
  worker takes over an idle `In Review` ticket after two hours.
- The reviewer and the author run on the same model family. The
  independence comes from a fresh context and a different brief, not from
  a different model.
- In a cloud session the GitHub proxy appends a
  `Generated by Claude Code` footer to every review comment and
  re-appends it on every edit. The owner decided on 2026-09-29 that
  reviews and review comments stay as posted, footer included; it is not
  a CLAUDE.md rule 9 breach to report or fix (GLG-769).

## Consequences

- No webhook, no polling and no timeout merge. The review of a head
  starts when the merger asks for it and ends with a report.
- One reader per diff instead of two. Review quality depends on
  `.claude/agents/pr-reviewer.md`; a change to what counts as a finding is
  made there, in a PR.
- Fewer comments. Problems in old code and anything a linter would say
  never reach a PR thread.
- The decision of ADR 0015 to run no GitHub Actions and no CI stands.

> Superseded 2026-10-14 (GLG-1373): it no longer stands. GitHub Actions runs the check set on every pull request and on `main` (GLG-1372, GLG-1374), and the required checks are the merge bar.

## Alternatives rejected

| Option                                         | Why not                                                                                                              |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Keep the routine, change only its prompt       | Keeps the webhook, the polling, the verdict parsing and the race between runs for no gain over a subagent.           |
| A one-line "reviewed" marker comment per head  | One more comment on every PR to carry information the merger already has from the reviewer's report.                 |
| Merger keeps its own full diff read            | Two reads of the same diff by the same model family; the merger's time goes to the checks, hygiene and triage.       |
| Local sessions only, the cloud worker no merge | Every PR would wait for a local session; the worker can spawn the same reviewer from the same file in the same repo. |
