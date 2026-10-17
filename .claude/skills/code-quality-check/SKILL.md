---
name: code-quality-check
description: Use when the user asks for a code quality check, quality audit or health check of the gelirgider codebase, or runs /code-quality-check. Not for reviewing one pull request (that is the pr-reviewer agent) and not for bug or security hunting.
---

# Code quality check

Two independent read-only audits, one for the backend and one for the frontend, then one short summary. Nothing is fixed, committed or ticketed by this skill.

## Steps

1. Spawn two agents in a single message so they run in parallel. Both: `subagent_type: "general-purpose"`, `model: "opus"` (Opus 5.5). Do not audit inline and do not merge the two into one agent.
   - Backend agent: scope `backend/` (Go, including `cmd/symbols`). Linters today: the ones `docs/PRINCIPLES.md` #6 lists for `make lint` (golangci-lint is configured in `backend/.golangci.yml`).
   - Frontend agent: scope `frontend/src/` (React + TypeScript). Linters today: `frontend/eslint.config.js`, `jscpd` (copied blocks of 6 lines or more, configured in `frontend/.jscpd.json`) and `knip` (unused exports and types, configured in `frontend/knip.json`), all run by `make lint`.
2. Give each agent the brief below with its scope and linter line filled in. If the user passed an argument (a path or a topic), add it to both briefs as a narrower scope.
3. Wait for both reports. Do not start the summary with one report missing.
4. Write the summary in the format below.

## Agent brief

```
Audit <scope> of this repository for code quality. Read-only: do not edit, commit, or write to Jira or GitHub.

Read first: CLAUDE.md, docs/PRINCIPLES.md, and the ADR in docs/adr/ that covers an area before you judge a design choice there. A choice recorded in an ADR, or marked with a `shortcut:` comment, is not a finding.

Look for exactly these four things:
1. Simplification: code that a shorter construct, the standard library, an installed dependency or an existing helper in this repo replaces; duplicated logic; dead code.
2. Performance: only with a concrete mechanism and a realistic trigger (query in a loop, O(n²) over transactions or price history, repeated parsing, a render or refetch on every keystroke, an oversized bundle chunk). No micro-optimizations.
3. Code smell: long functions, deep nesting, boolean-flag parameters, primitive obsession, unclear names, inconsistent handling of the same case in sibling code.
4. Misused abstraction: an interface with one implementation, a wrapper that adds nothing, a hook or component split that hides a simple flow, a layer that leaks (SQL outside the repository layer, fetch logic in a presentational component), a pattern applied where a function would do.

Not in scope: bugs, security, formatting, missing tests.

Start from the largest and most-changed files, then follow what you find. Confirm every finding in the code (read the callers) before you report it. Report the ten findings with the highest payoff at most, ranked. Fewer is fine.

For each finding, name a linter change that would catch this class of mistake. Current setup: <linter line>. Prefer a rule in a linter or plugin that is already installed; next a well-known linter or plugin that is not installed (say that it is a new dependency). Give the exact rule name and options, and check that the rule exists. If no rule catches it, write "none".

Report format, one block per finding, nothing else:
<path>:<line> | <simplification|performance|smell|abstraction>
Problem: one sentence.
Fix: one sentence.
Lint: <rule and config file> or none
```

## Summary format

```
## Backend (<n> findings)
1. `path:line` category: problem. Fix: fix.
...

## Frontend (<n> findings)
1. ...

## Linter changes
- <config file>: <rule and options>. Catches: backend 1, 4.
- New dependency: <linter>, <rule>. Catches: frontend 2.
```

- Keep each agent's ranking. One line per finding.
- Merge linter suggestions that name the same rule, and list which findings each one catches. Leave out findings with `Lint: none` from that section.
- Drop a finding that has no `path:line`. Add no findings of your own.
- No praise, no preamble, no closing advice. A side with no findings gets the line "No findings."
