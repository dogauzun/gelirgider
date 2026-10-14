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

Every PR, except one whose diff is exempt (below), is reviewed by a separate
review agent, `pr-reviewer`, on Opus 5.5 at high effort. The merger spawns it
as a subagent: the hourly kanban worker in the cloud, the orchestrating
session locally. It runs once the PR is open, alongside the required GitHub
Actions checks, not after them: a lint failure rarely changes a finding, and
waiting for the checks added their run time to the review time of every PR.
The checks stay a condition for the merge. A fresh review runs after every
push that changes the PR's diff, which includes a push that fixes a failing
check. The agent that wrote the code never reviews it, and the reviewer
starts without the author's context.

### Exempt diffs

The reviewer posts findings on about 15 to 20% of PRs, and a docs-only review
once took over an hour. A PR whose diff touches only the paths below skips
the reviewer; the required checks and the merger's hygiene step suffice. The
list is closed and decided by the file paths of the diff
(`gh pr diff <n> --name-only`), never by the PR title type. It is defined only
here; CLAUDE.md and `docs/PRINCIPLES.md` point to this section.

- `docs/`, except `docs/PRINCIPLES.md` and `docs/adr/`, and the root `*.md`
  files, except `CLAUDE.md` (so `README.md`, `README.tr.md`,
  `CONTRIBUTING.md`, `SECURITY.md` and `docs/ROADMAP.md` are exempt);
- `.github/`;
- `deploy/`;
- `frontend/src/i18n/`, as a directory, so the rule holds whether
  `strings.ts` is one file or several;
- chore files, by name: `.gitignore`, `.gitattributes`, `.dockerignore`,
  `LICENSE` and `THIRD_PARTY_NOTICES.md`. A PR may add another plain data
  file to this list, never a `Makefile`, a dependency manifest or a lockfile.

Everything else is reviewed: the rule documents (`CLAUDE.md`,
`docs/PRINCIPLES.md`, `docs/adr/`), because the merger's hygiene step does not
compare documents with each other and the reviewer's findings on them have
been real inconsistencies; `.claude/`, because agent files, skills and routine
prompts steer agents; the `Makefile`, dependency manifests and lockfiles;
`backend/`; and `frontend/` outside `frontend/src/i18n/`. A diff that touches
one path outside the list is reviewed in full.

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

1. decides from `gh pr diff <n> --name-only` whether the diff is exempt, and
   spawns the reviewer as soon as the PR is open when it is not;
2. reads the required GitHub Actions checks on the PR head, once they have
   run: `Lint (Go + web)`,
   `Go tests`, `Web tests` and `Web build` (workflow `ci`) and `Title`
   (workflow `pr`), see `docs/PRINCIPLES.md` #6;
3. checks hygiene: no AI attribution in commits or the PR body, the PR body
   follows the template, non-trivial logic
   ships with a test, a bug fix's Testing section names a test that failed
   before the fix or says why no test can express the bug, and a new or
   changed route has its swag annotation (the missing tests and documentation
   the reviewer leaves to the merger);
4. reads the reviewer's report (an exempt PR has none);
5. sends findings that need a code change back to the ticket's agent, and
   answers the others in their thread: a disagreement with evidence, or a
   new GLG ticket for a real but out-of-scope problem (CLAUDE.md rule 4);
6. resolves every thread once it is fixed or answered.

### Merge gate

A PR merges when all of these hold (CLAUDE.md rules 6 and 7):

1. the reviewer has reviewed the PR's current diff: the current head, or
   the head before a push that only rebased the branch onto `main` and
   resolved no conflict; an exempt diff satisfies this item without a
   review, and a push that adds a non-exempt path ends the exemption;
2. every review thread, from the reviewer or a human, is fixed or
   answered and resolved;
3. the required checks are green on the current head. The merger turns on
   auto-merge (`gh pr merge --auto --squash --subject "<PR title> (#<n>)" --body "<the PR's Description text>"`) and GitHub merges as soon as
   they pass; the branch does not have to be up to date with `main`, so no
   rebase is needed first. A push run on `main` that fails after a merge is
   a bug ticket for the merger that landed it. Auto-merge is turned on only as the last step, after the reviewer has reviewed the current head (or the diff is exempt) and every thread is resolved; before any push that changes the diff after that, the merger runs `gh pr merge --disable-auto` and turns auto-merge on again only after the new review.

A push that changes the PR's own diff gets a new review. A push that only
rebases the branch onto `main` does not, and neither does a finding that
was answered without a push. A rebase that had to resolve a conflict
rewrites the PR's own lines, so it counts as a change and gets a review.

A PR branch changes only through git under the owner's identity, never
through the GitHub contents API or the update-branch endpoint, which decode
escape sequences and add merge commits. A merge passes an explicit commit
title and message, because the merge tool adds a `Co-authored-by` trailer
otherwise (CLAUDE.md rule 9) and the repository's squash defaults would
land a single-commit PR under the commit's subject and copy branch commit
trailers into `main`.

A round is one review that posted findings; after six rounds on one PR
the merger parks the ticket with the `needs-decision` label instead of a
seventh.

## Recorded simplifications

- Nothing on GitHub says that a clean PR was reviewed. The merger's merge
  comment states either `reviewer skipped: exempt paths` or the head the
  reviewer ran on.
- A review that starts while the checks run is wasted when a failing check
  forces a push that changes the diff. The cost is accepted; the cost line
  the merger records per ticket shows how often it happens.
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

- One reader per non-exempt diff. Review quality depends on
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
