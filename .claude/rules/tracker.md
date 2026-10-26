# Tracker

The gelirgider Tasks database in Notion ([ADR 0040](../../docs/adr/0040-tracker-moved-to-notion.md)). Who moves a task and when, branch names, the PR title and the `PR` property are in `CLAUDE.md` (rules 1, 2, 5, 10, 12); this file holds the identifiers, the schema and the query recipes.

## Identifiers

- Database id `6e5a7046a91c46a8bd19fbd6ad737cf0`, data source `collection://96301f5d-9a95-48ac-8381-72392034e31c`, ID prefix `GLG`.

## Schema

- `Name` (title).
- `ID` (unique id, read-only; SQL column `"userDefined:ID"`).
- `Status` (status: `To Do`, `In Progress`, `In Review`, `Done`; default `To Do`).
- `Type` (select: `Task`, `Bug`, `Epic`).
- `Labels` (multi-select: `needs-decision`, `blocked`, `qa`, `docs`, `code-quality`, `security`, `build`).
- `Epic` / `Children` (self-relation), `Blocked by` / `Blocks` (self-relation).
- `PR` (url), `Updated` (last edited time).
- Views: Board (by Status, epics hidden), Needs decision, Blocked, Open epics, Done this week.

## Meaning

- `needs-decision`: a question waits for the owner. `blocked`: the owner has not released the task. Sweeps skip both.
- Epics are containers, never work items. A task hangs under its epic through `Epic`.
- A duplicate is closed with a comment naming the survivor and status `Done`.
- GitHub issues stay the entry for outside contributors; a maintainer re-files an accepted issue as a task and links both ways.

## House style

- Body: `## What` / `## Scope` / `## Acceptance` / `## Depends on` / `## Notes`. English. One day of work at most. Synthetic figures only: task text gets quoted into public PRs and commits.

## Recipes

Use `query-data-sources` in SQL mode, with the data source URL as the table name.

- Board: `SELECT "userDefined:ID" AS id, Name, Status, Type, Labels, Epic, PR, url FROM [collection://96301f5d-9a95-48ac-8381-72392034e31c] WHERE Status != 'Done' ORDER BY "userDefined:ID"`.
- Task of a PR (the PR body carries no task link; the `PR` property is the one link between the two): `SELECT "userDefined:ID" AS id, Name, Status, url FROM [collection://96301f5d-9a95-48ac-8381-72392034e31c] WHERE PR = '<PR URL>'`.
- To Do sweep: the Board query with `AND Status = 'To Do' AND Type != 'Epic' AND Labels NOT LIKE '%needs-decision%' AND Labels NOT LIKE '%blocked%' AND ("Blocked by" IS NULL OR "Blocked by" = '[]')`. Relation columns need the data source fetched in the session first.
- Decisions sweep: `WHERE Status != 'Done' AND (Labels LIKE '%needs-decision%' OR Labels LIKE '%blocked%')`.
- Filing: `create-pages` with parent `{"type":"data_source_id","data_source_id":"96301f5d-9a95-48ac-8381-72392034e31c"}`, properties `Name`, `Type`, `Labels`, `Epic` (array of page URLs) and `Blocked by` (array of page URLs), the body as content.
- Duplicate check after a filing wave (a silent retry of `create-pages` may duplicate; this is unknown): `SELECT "userDefined:ID", Name FROM [collection://96301f5d-9a95-48ac-8381-72392034e31c] WHERE "userDefined:ID" >= <first id of the wave> ORDER BY Name`, then look for repeated names.
- Transitions: `update-page` with `update_properties` and the status name.

The Notion calls (create, query, transition, set `PR`, delete) work from a Claude Code session and from a general-purpose subagent alike, with no permission or classifier denial. Who writes is `CLAUDE.md` rule 12: the coordinating session, never the implementer.
