---
name: code-quality-check
description: Use when the user asks for a code quality check, quality audit or health check of the gelirgider codebase, or runs /code-quality-check. Not for reviewing one pull request (that is the pr-reviewer agent) and not for bug or security hunting.
---

# Code quality check

Two independent read-only audits, one for the backend and one for the frontend, then one summary that grades every finding by severity and suggests tickets. Nothing is fixed, committed or ticketed by this skill: it only suggests tickets.

## Steps

1. Spawn two agents in a single message so they run in parallel. Both: `subagent_type: "general-purpose"`, `model: "opus"` (Opus 5.5). Do not audit inline and do not merge the two into one agent.
   - Backend agent: scope `backend/` (Go, including `cmd/symbols`). Linters today: the ones `docs/PRINCIPLES.md` #6 lists for `make lint` (golangci-lint is configured in `backend/.golangci.yml`).
   - Frontend agent: scope `frontend/src/` (React + TypeScript). Linters today: `frontend/eslint.config.js`, `jscpd` (copied blocks of 6 lines or more, configured in `frontend/.jscpd.json`) and `knip` (unused exports and types, configured in `frontend/knip.json`), all run by `make lint`.
2. Give each agent the brief below with its scope and linter line filled in. If the user passed an argument (a path or a topic), add it to both briefs as a narrower scope.
3. Wait for both reports. Do not start the summary with one report missing.
4. Grade every finding high, medium or low with the rubric below, and group the findings into suggested tickets with the ticket rules below.
5. Write the summary in the format below.
6. Create no ticket. Only an explicit approval from the owner in chat, naming the tickets, starts that, and it covers only the tickets named.

## Agent brief

```
Audit <scope> of this repository for code quality. Read-only: do not edit, commit, or write to Jira or GitHub.

Read first: CLAUDE.md, docs/PRINCIPLES.md, and the ADR in docs/adr/ that covers an area before you judge a design choice there. A choice recorded in an ADR, or marked with a `shortcut:` comment, is not a finding.

Look for exactly these four things:
1. Simplification: code that a shorter construct, the standard library, an installed dependency or an existing helper in this repo replaces; duplicated logic; dead code.
2. Performance: only with a concrete mechanism and a realistic trigger (query in a loop, O(n²) over transactions or price history, repeated parsing, a render or refetch on every keystroke, an oversized bundle chunk). No micro-optimizations.
3. Code smell: long functions, deep nesting, boolean-flag parameters, primitive obsession, unclear names, inconsistent handling of the same case in sibling code.
4. Misused abstraction: an interface with one implementation, a wrapper that adds nothing, a hook or component split that hides a simple flow, a layer that leaks (SQL outside the repository layer, fetch logic in a presentational component), a pattern applied where a function would do.

Not in scope: bugs, security, formatting, missing tests. One exception: when a duplication or an inconsistency between sibling code already makes one copy behave differently (a double submit one copy guards against, a 404 where a sibling answers 500), report it as that finding and say the wrong behavior in Problem.

Start from the largest and most-changed files, then follow what you find. Confirm every finding in the code (read the callers) before you report it. Report the ten findings with the highest payoff at most, ranked. Fewer is fine.

For each finding, name a linter change that would catch this class of mistake. Current setup: <linter line>. Prefer a rule in a linter or plugin that is already installed; next a well-known linter or plugin that is not installed (say that it is a new dependency). Give the exact rule name and options, and check that the rule exists. A `no-restricted-syntax` selector written for this one finding does not count. If no rule catches it, write "none".

Report format, one block per finding, nothing else:
<path>:<line> | <simplification|performance|smell|abstraction>
Problem: one sentence.
Fix: one sentence.
Lint: <rule and config file> or none
```

## Severity

Grade by the cost of leaving the code as it is, not by the size of the fix.

- High: two or more copies of a money, return, price or error-handling rule that can drift apart and show a wrong figure or status, or code that lets a user action run twice.
- Medium: duplication or a leaking layer that every change in that area pays for, a classification that a new case can silently miss, or a function at its lint cap.
- Low: a local cleanup: a dead parameter or result, a pass-through wrapper, an interface with one implementation, one small duplicate.

## Ticket rules

- One ticket per finding, except: findings that change the same code, or that one reviewer would read as one mechanical change (CLAUDE.md workflow rule 3), share a ticket.
- A ticket's severity is its highest finding's. Type `fix` when a finding describes wrong behavior (a double submit, a 404 for a DB error), `refactor` otherwise.
- A linter change that flags only the findings' code goes in the ticket that fixes them. A rule that also flags other code gets its own ticket, after the tickets whose findings it covers, so `make lint` never goes red on `main`.
- When two tickets change the same file (adding a rule to the same lint config does not count), name the order (`after <ticket>`); it becomes a Blocks link.

## Summary format

```
## High (<n>)
1. B1 `path:line` category: problem. Fix: fix.
...

## Medium (<n>)
...

## Low (<n>)
...

## Suggested tickets
1. `refactor: <what changes>` (high). Findings: B1. Lint: <rule> or none. After: <ticket number> or nothing.
...

## Linter changes
- <config file>: <rule and options>. Catches: B1, B4.
- New dependency: <linter>, <rule>. Catches: F2.
```

- Label each finding by side and its rank in that agent's report (B1 is the backend agent's first, F4 the frontend's fourth), and keep that order inside each severity. One line per finding.
- Merge linter suggestions that name the same rule, and list which findings each one catches. Leave out findings with `Lint: none` from that section.
- Drop a finding that has no `path:line`. Add no findings of your own.
- No praise, no preamble, no closing advice. A severity with no findings gets the line "No findings."
