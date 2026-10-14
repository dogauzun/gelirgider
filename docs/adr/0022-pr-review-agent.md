# ADR 0022: PR review by a subagent of the merger

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

### A review subagent, spawned by the merger

Every PR is reviewed by a separate review agent, `pr-reviewer`, on Opus 5.5 at
high effort. The merger spawns it as a subagent: the hourly kanban worker in
the cloud, the orchestrating session locally. It runs once the PR is open and
its required GitHub Actions checks are green on the PR head, and a fresh one
runs after every push that changes the PR's diff. The agent that wrote the
code never reviews it, and the reviewer starts without the author's context.

The author is the `ticket-implementer` agent, on Sonnet 5.5 at high effort.

Both agents' instructions live in the repository, in
`.claude/agents/pr-reviewer.md` and `.claude/agents/ticket-implementer.md`,
so the cloud worker and a local session use the same text. A session whose
harness does not load those files as agent types spawns a general subagent
with the same model and effort and tells it to read the file and follow it.

No session waits for or parses a verdict. The review of a head starts when
the merger asks for it and ends with a report.

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
authored by the account the agents run as.

### What the merger does

The merger does not read the whole diff. It:

1. reads the required GitHub Actions checks on the PR head: `Lint (Go + web)`,
   `Go tests`, `Web tests` and `Web build` (workflow `ci`) and `Title`
   (workflow `pr`), see `docs/PRINCIPLES.md` #6;
2. checks hygiene: no AI attribution in commits or the PR body, the PR body
   follows the template, the branch contains current `main`, non-trivial logic
   ships with a test, a bug fix's Testing section names a test that failed
   before the fix or says why no test can express the bug, and a new or
   changed route has its swag annotation (the missing tests and documentation
   the reviewer leaves to the merger);
3. spawns the reviewer and reads its report;
4. sends findings that need a code change back to the ticket's agent, and
   answers the others in their thread: a disagreement with evidence, or a
   new GLG ticket for a real but out-of-scope problem (CLAUDE.md rule 4);
5. resolves every thread once it is fixed or answered.

### Merge gate

A PR merges when all of these hold (CLAUDE.md rules 6 and 7):

1. the reviewer has reviewed the PR's current diff: the current head, or
   the head before a push that only rebased the branch onto `main` and
   resolved no conflict;
2. every review thread, from the reviewer or a human, is fixed or
   answered and resolved;
3. the required checks are green on the current head, the commit that
   merges. GitHub runs them again on every push, and `main` requires the
   branch to be up to date.

A push that changes the PR's own diff gets a new review. A push that only
rebases the branch onto `main` does not, and neither does a finding that
was answered without a push. A rebase that had to resolve a conflict
rewrites the PR's own lines, so it counts as a change and gets a review.

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
  re-appends it on every edit. The owner decided that reviews and review
  comments stay as posted, footer included; it is not a CLAUDE.md rule 9
  breach to report or fix.

## Consequences

- One reader per diff. Review quality depends on
  `.claude/agents/pr-reviewer.md`; a change to what counts as a finding is
  made there, in a PR.
- Fewer comments. Problems in old code and anything a linter would say
  never reach a PR thread.

## Alternatives rejected

| Option                                         | Why not                                                                                                              |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| A cloud routine fired by a webhook and a timer | The webhook, the polling, the verdict parsing and the race between runs, for no gain over a subagent.                |
| A one-line "reviewed" marker comment per head  | One more comment on every PR to carry information the merger already has from the reviewer's report.                 |
| Merger keeps its own full diff read            | Two reads of the same diff by the same model family; the merger's time goes to the checks, hygiene and triage.       |
| Local sessions only, the cloud worker no merge | Every PR would wait for a local session; the worker can spawn the same reviewer from the same file in the same repo. |
