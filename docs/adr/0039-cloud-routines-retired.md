# ADR 0039: Cloud routines retired

Status: accepted (2026-10-23).

## Context

Five scheduled cloud routines worked on the repository without a session the
owner had started:

- the kanban worker, hourly: picked up ready tickets, had a subagent implement
  each, saw the pull requests reviewed and merged them;
- the daily QA run: tested the application end to end and filed tickets for
  the bugs it confirmed;
- the daily roadmap scout: compared the product with similar trackers and
  filed epics for the two best gaps;
- the weekly doc check: read the documentation against the code and filed a
  ticket for each inconsistency;
- the weekly security review: read the codebase and filed the High-severity
  defects.

Their full instructions lived in `.claude/routines/`. Jira remains the
tracker for now.

## Decision

The owner retired every cloud routine on 2026-10-23 and may bring them back.
`.claude/routines/` is deleted. The merger, the QA run, the security review
and the documentation check run from sessions the owner starts. The review of a
pull request is the `Claude Code Review` workflow ([ADR 0037](0037-pr-review-workflow.md)),
which does not depend on a routine.

## Open questions

None.
