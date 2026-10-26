# ADR 0040: Tracker moved from Jira to Notion

Status: accepted (2026-10-26).

## Context

Work was tracked in the Jira project `GLG` on stockpuppet.atlassian.net: a
ticket per change, its key in the branch name and the PR title, its
transition timestamps as the effort record. The agents reached it through the
Atlassian MCP connector, whose calls the harness denied inside subagents, so
only the coordinating session could write to it. Notion is the maintainer's
workspace already, and one MCP connector reaches it from every subagent.

## Decision

- The tracker is one Notion Tasks database per project; gelirgider's is the
  database "gelirgider Tasks" on the page "Projects". A ticket is a task page there,
  with Status `To Do` / `In Progress` / `In Review` / `Done`, Type `Task` /
  `Bug` / `Epic`, labels, an `Epic` / `Children` relation, a `Blocked by` /
  `Blocks` relation and a `PR` URL property. The database ids, label meanings,
  house style and query recipes are in `.claude/rules/tracker.md`.
- Nothing is imported from Jira; open Jira tickets are not carried over, and
  the Jira project is left as it is.
- No task key in branch names or PR titles. The PR body's `Ticket:` line holds
  the task page URL (or a GitHub issue for outside contributors), the squash
  body carries it into git, and the task's `PR` property holds the PR URL from
  the moment the PR opens.
- No effort record. Status transitions by name are the only progress signal;
  nothing is estimated or timed.
- GitHub issues stay the entry for outside contributors.
- The Notion unique IDs restart at 1 with the prefix `GLG`. A `GLG-n` key in
  git history, an ADR or a memory ledger dated before 2026-10-26 refers to the
  closed Jira project `GLG`, not to a Notion task.

## Open questions

None.
