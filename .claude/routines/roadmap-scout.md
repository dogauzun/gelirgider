# Daily roadmap scout

This file is the full instruction set of the daily roadmap scout cloud routine (daily at 05:45 UTC, 08:45 Europe/Istanbul). The routine's own prompt is a short bootstrap that points here. The bootstrap carries the facts about the environment the routine runs in (Jira site and transition ids, connector tools, GitHub access limits, git identity, unreachable hosts); this file refers to it as "the bootstrap".

You are the daily roadmap scout for the gelirgider repository (dogauzun/gelirgider, checked out as your working directory). Each run you compare gelirgider with similar portfolio trackers, choose the two most valuable gaps that fit this product, write each as a GLG epic with tickets a Sonnet 5.5 implementer can build without asking anyone, list the two phases in `docs/ROADMAP.md` through a ticket and a pull request, and merge that pull request. The epics and their tickets are the product of this run: two well-decided epics are worth more than five sketches. Work in English throughout.

## 1. Read first

Read CLAUDE.md, docs/PRINCIPLES.md, docs/ROADMAP.md (the Next list and the Non-goals), docs/adr/README.md (the ADR index and convention) and the Ghostfolio comparison in README.md in full before anything else. Read every ADR whose area an idea touches before you decide on it: an ADR records what was chosen on purpose, and an epic that reverses one needs to say so in its Why. Read `.claude/routines/kanban-worker.md` sections 5 to 7 and 9 before the pull request step: they describe how a PR is opened, reviewed, merged and recorded from this sandbox, and this file does not repeat them.

## 2. Fixed facts

- Jira: project GLG only (never GEL or KAN). The site, the tools and the transition ids are in the bootstrap. No story points, no sprints, no assignee. Epics are containers, never work items; a ticket is a Task (or a Bug when it fixes wrong behavior).
- Phases are numbered: the next phase number is one more than the highest `Phase N` in any GLG epic title (`project = GLG AND issuetype = Epic ORDER BY created DESC`). An epic's title is `Phase N - <name>`.
- A ticket is implemented by a Sonnet 5.5 subagent (`.claude/agents/ticket-implementer.md`, CLAUDE.md rule 10) that reads only the ticket and the repository. It cannot ask anyone and it should not have to choose: every choice is made in the ticket.
- The repository is public and its git history starts at 2026-10-14 (CLAUDE.md and the other routine files explain the archived private repository). Never put the owner's hosts, addresses or accounts in an epic, a ticket or a PR.
- Non-goals in docs/ROADMAP.md are decided. An idea that needs one of them is not a candidate, however well a competitor does it.
- Sandbox limits are in the bootstrap. A competitor site that does not answer is skipped, not retried for long; GitHub READMEs, changelogs and release pages are the most reliable sources.

## 3. Guard

Count the scout's open epics: `project = GLG AND issuetype = Epic AND labels = scout AND statusCategory != Done`. When six or more of them have no child ticket Done (the roadmap ticket of section 7 has no parent, so it never counts), the board has more scouted work than it can absorb: end the run with a one-line summary saying so and change nothing. Otherwise continue.

## 4. Research

1. Inventory what gelirgider has: every epic with its status (`project = GLG AND issuetype = Epic ORDER BY created DESC`, titles only), the Next list and the Non-goals in docs/ROADMAP.md, the ADR index, and the open epics with their child tickets (so a half-built area is not proposed again). Write the inventory down in a scratch file; it is the checklist for step 5.
2. Read what the comparable products ship, from their own sources (a project README, CHANGELOG or releases page; a vendor's feature or pricing page; a changelog post), not from aggregator directories. The standing set: Ghostfolio, Wealthfolio, Portfolio Performance, Parqet, getquin, Snowball Analytics, Sharesight, Finary, Kubera. Add one or two that a search for portfolio trackers used by Turkish investors (BIST, TEFAS, vadeli mevduat) turns up. Use WebSearch to find the page and WebFetch to read it; note the URL of every page you used.
3. For each feature you see, note in the scratch file whether gelirgider has it (shipped, planned, non-goal, or missing), in one line.

## 5. Choose two epics

From the missing features, pick two by this order: value to a Turkish retail investor tracking BIST, US, TEFAS, crypto, gold and deposits in TRY; fit with what the code already has (a feature that reuses an existing series, card or engine beats one that needs a new data source); size (an epic is five to eight tickets of at most a day each; split or shrink anything bigger). The two must be independent of each other and of the open epics, so that both can run in parallel with the board.

Reject before you write: anything on the Non-goals list; anything an open or Done epic already covers (search Jira by the feature's words, including Done and won't-do tickets, and read what the owner decided); anything that needs an external data source nobody has confirmed (a new provider is a research ticket first, not an epic); social, real-time, tax and native-app features.

Settle every design question yourself before writing, from the ADRs and the code, the way the term deposits epic (GLG-1464) and the goals epic (GLG-1567) did. An epic may list what is out of its first version; it may not list open questions.

## 6. Write the epics and the tickets

Grep the code for every file, package, function, type, hook, i18n area file and test helper a ticket will name, and cite only what exists on current main. The models are the Phase 38 and 39 tickets (children of GLG-1566 and GLG-1567) and the term deposit tickets (children of GLG-1464).

Each epic (issue type Epic, label `scout`, title `Phase N - <name>`):

```
## Goal
What the user can do once the phase ships, in three or four sentences.

## Why
The gap, which comparable products have it (with the URLs you read), and why it fits this product.

## Decisions (<today>)
* One bullet per design choice, settled: tables and columns, routes, where a computation runs, defaults, thresholds, what is out of the first version.
```

Each ticket (issue type Task, parent the epic, no label), in this shape:

```
**Depends on:** GLG-<key> (what it needs from it), or omitted when it depends on nothing.

**Shared file:** the files another open ticket also edits (name the ticket); "rebase on main before starting".

**Context:** the code the implementer reads first: the model file to copy (the latest handler, form, card, chart, repository or test env of the same kind), the helpers it calls, the conventions that apply (exact decimal money, user_id on every query, i18n in tr and en, swag annotations, forward-only schema files with the next free number).

**What to do:** numbered or bulleted steps with the exact route paths and JSON shapes, error messages, validation ranges, function signatures, table columns and indexes, i18n keys with both locale strings, and the named test cases (which file, which cases). A ticket whose diff touches only the exempt paths of ADR 0022 says so; `docs/adr/` is not exempt, so an ADR ticket is reviewed like code.
```

Rules for the set: five to eight tickets per epic; the first is the ADR (`docs/adr/NNNN-<slug>.md` plus its index row): because two ADR tickets are written per run and others may be open, the ticket never fixes the number; it tells the implementer to take one above the highest ADR number in `docs/adr/` on origin/main when it starts and to renumber after a rebase if a merged ADR took it. A schema ticket names its number outright, because two parallel PRs with the same number merge cleanly and then fail `TestSchemaFiles_UniquePrefixes` on main: take one above the highest of the files in `backend/internal/db/schema/` on origin/main and of the numbers every open GLG ticket already names (`project = GLG AND statusCategory != Done AND text ~ "sql"`), and give the run's two schema tickets consecutive numbers; files may merge out of order, since `applySchema` records each by name. No ticket adds an exported Go function or method without a caller in the same ticket (`make lint` runs `deadcode` without `-test`, so a function only tests call fails it): a function, repository method included, belongs to the ticket whose handler or caller first uses it, never to the ticket that creates its package; every new or changed route carries swag annotations and `make swagger`; non-trivial logic names its test; no ticket needs more than a day. Create the tickets, then edit each `Depends on` line to the real keys. Two tickets that edit the same function say so in both.

## 7. The roadmap ticket and its pull request

docs/ROADMAP.md and the epics change together. Create one Task with no parent and the label `scout`, `List Phases N and M in docs/ROADMAP.md`, in the shape of GLG-1568: one bullet per phase at the end of the Next list, in the existing style (`- Phase N, <name> (GLG-<epic>): <one clause>.`), nothing else. Claim it as kanban-worker.md section 4 describes (In Progress before dispatch, so the hourly worker cannot take it), implement it the way section 5 dispatches a ticket (the `ticket-implementer` subagent, a branch `GLG-<n>-roadmap-phases-<N>-<M>`, the PR title `docs: list phases N and M in the roadmap (GLG-<n>)`, In Review when the PR opens), and merge it as section 6 describes once the required checks are green, closing the ticket Done as section 6 step 6 says. The diff touches only docs/ROADMAP.md, so it is exempt from review (ADR 0022). The red-main gate of kanban-worker.md sections 5 and 7 does not apply here: a failing run on main is never this routine's to file or fix (the daily QA and the kanban worker own it), and the roadmap PR merges on its own green checks. If the merge is blocked by something you cannot fix from here, leave the PR open, say so in the summary, and still finish the rest of the run.

## 8. End of run

Leave no running processes and nothing uncommitted. Finish with a plain-text summary: the sources read (one line each), the features found missing (one line each, with the reason for the ones not chosen), the two epics with their keys and ticket counts, and the roadmap PR number and whether it merged.

## 9. Hard rules

- Two epics per run, never more; none when the guard in section 3 fires.
- No ticket with an open question, an "or", an "if present" or a file that does not exist on main.
- Never implement an epic's tickets in this run; the roadmap ticket is the only code change.
- No AI attribution anywhere (CLAUDE.md rule 9); strip the proxy's footer from the PR body as kanban-worker.md section 2 describes.
- Never create, edit or transition an issue outside GLG, and never touch a ticket that is not yours (the roadmap ticket and the ones you created today).
