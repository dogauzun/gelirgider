# Guiding principles

## 1. Ship the smallest thing that works

- Ask first: does this need to exist? If speculative, skip and say so.
- Reuse what is already in the repo. Then stdlib. Then an installed dependency. New dependency last.
- No interface with one implementation. No config for a value that never changes. No scaffolding "for later".
- Deletion beats addition. Boring beats clever.
- Before writing, read the task and the code it touches and list every place the change must reach: callers, tests, fixtures, config, exports. Check what it could break for users (data destroyed or exposed, callers that stop working). That is the scope; extra features are not.
- Take the first option that fully works: (1) does it need to exist, name what you skip in one line; (2) already in this codebase, used the way the surrounding code uses it; (3) stdlib or platform feature, unless the project has its own (a house component beats a native widget); (4) an installed dependency, never a new one for a few lines; (5) one line a reader gets at a glance; (6) otherwise the minimum code that works.
- Lazy about the solution, never about the change: finish every part the task needs, including the callers, tests and fixtures the change breaks.
- No abstraction, wrapper, type conversion, option, config, boilerplate or "for later" code nobody asked for. Keep values in the form the platform gives them. Keep the structure the codebase has: its layers, interfaces and conventions.
- The shortest working diff wins once everything it must touch is known; a one-liner that needs decoding is not short. Comment only the why the code cannot show, in one line.
- Bug fix: grep every caller of the function you touch before editing, then fix the root cause once in the shared code. Code you move or merge keeps its error handling and validation. Between options of equal size, take the one that is correct on edge cases.
- A deliberate shortcut with a known limit gets a code comment `shortcut: <the limit>, <when to upgrade>`; the ponytail command `/ponytail-debt` lists them.
- Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything the task asks for.
- This is the ruleset of the ponytail plugin (DietrichGebert/ponytail, its `AGENTS.md`); it lives here so agents that run without the plugin follow the same text.

## 2. Multi-user from the first migration

- Single developer today, many users tomorrow. Schema and queries are user-scoped from day one.
- Every user-data table carries `user_id`. Every read and write filters by it at the repository layer.
- No global state that assumes one user (caches, cron jobs, in-memory holdings).

## 3. Money is exact

- `decimal` everywhere. Never `float64` for price, quantity, cost, or value.
- Store currency next to every amount. Default `TRY`. Supported: `TRY`, `USD`, `EUR`.
- Prices and quantities keep source precision (BIST 2 decimals, TEFAS up to 6).

## 4. SQLite now, portable SQL always

- SQLite in production, one instance, one file. See [ADR 0002](adr/0002-production-datastore-and-migrations.md).
- Nothing outside the repository layer knows which database is in use. That keeps a later move cheap.
- Write portable SQL. Avoid dialect-only features, triggers, and stored procedures. Business logic lives in Go.
- Schema changes are plain `.sql` files applied in filename order at startup. Forward-only. No migration tool.

## 5. Small, independent units of work

- One task, one PR, one concern. Vertical slices over horizontal layers when possible.
- Tasks are written so they can be worked in parallel. Name the dependency if one truly exists.
- Unrelated problem found mid-task: open a new task. Never widen the PR.

## 6. Verified and reviewed before merged

- The required GitHub Actions checks green on the PR head is the bar: `Go lint`, `Web lint`, `Go tests`, `Web tests` and `Web build` run the Make targets `lint-backend`, `lint-frontend`, `test-backend`, `test-frontend` and `build-frontend` (`.github/workflows/checks.yml`, workflow `ci`, listed below), `Title` checks the title (workflow `pr`, `.github/workflows/pr-title.yml`), and `Claude review` is the review below (workflow `Claude Code Review`, `.github/workflows/claude-code-review.yml`). On a pull request the checks are change-scoped: `Go lint` and `Go tests` do their work only when `backend/` changed, `Web tests` and `Web build` only when `frontend/` changed, `Web lint` when `frontend/`, `docs/`, `deploy/`, `.claude/`, `.github/` or a root `*.md` changed (it owns the prettier check on those paths), a `Makefile` or `.github/workflows/` change runs all of them, and a skipped check still reports green; pushes to `main` run everything. The author still runs `make lint`, `make test` and `make build-frontend` from the repo root before pushing.
  - `make lint` runs `make lint-backend` and `make lint-frontend`, which CI runs as separate jobs. `make lint-backend`: `make swagger-check`, `gofmt -l backend` (fails if it prints a file), `go vet ./...`, `golangci-lint run` (`dupl`, `forbidigo`, `funlen` with `lines` 100 (comments ignored, tests exempt), `goconst` with `min-len` 3 and `min-occurrences` 3 (tests exempt), `gocognit` with `min-complexity` 30, `nestif` with `min-complexity` 10, `noctx`, `revive` with `argument-limit` 6 and `file-length-limit` 1000 (comments ignored), `unparam`, and `usestdlibvars` with `time-layout`, configured in `backend/.golangci.yml`) and `deadcode ./...` (without `-test`, fails if it prints a function) in `backend/`. `make lint-frontend`: `npm run lint`, `npm run lint:dupl` (`jscpd`, configured in `frontend/.jscpd.json`), `npm run knip` (unused exports and types, configured in `frontend/knip.json`), `npx prettier --check .` in `frontend/`, prettier check on `docs/`, `deploy/`, `.claude/`, `.github/` and the root `*.md` files.
  - `make test`: `make test-backend` (`go vet ./...` and `go test ./...` in `backend/`) and `make test-frontend` (`npm run lint` and `npm test` in `frontend/`). CI runs the two as separate jobs.
  - `make build-frontend`: `npm run build` in `frontend/`. Neither `make lint` nor `make test` runs it.
- A PR merges through auto-merge once its head is reviewed and its threads are resolved: GitHub merges it as soon as the required checks are green on its head, with no rebase onto `main` first. A push run on `main` that goes red after a merge is a bug task for the merger that landed it. The procedure is in [CLAUDE.md](../CLAUDE.md) rule 7.
- Non-trivial logic ships with a test that fails when the logic breaks. One test, not a suite.
- A bug fix starts with the test. Before writing the fix, turn the bug report into a test that fails for the reported reason, run it and watch it fail. The fix is done when that test passes and the others stay green. The PR body names the test and states that it failed before the fix.
- When no automated test can express the bug (pure visual layout, a documentation fix), say so in the PR and describe the manual check instead.
- The PR body states the commands run and what was observed.
- Every PR's current diff is reviewed by the review agent (`.claude/agents/pr-reviewer.md`), which the workflow `Claude Code Review` runs on every push and the merger spawns for a PR from a fork, a Dependabot PR, one that changes the workflow file, or any PR while the owner has lifted the required check for an outage ([ADR 0037](adr/0037-pr-review-workflow.md); finding rules in [ADR 0022](adr/0022-pr-review-agent.md)). Its job `Claude review` is a required check, so GitHub holds the PR until the review has finished on the current head; a run that fails is re-run until it succeeds, and a lasting outage goes to the owner. A diff that touches only exempt paths (the list lives only in the workflow's `scope` step, ADR 0022) gets a run that ends `success` without a review and says so in its job summary, so `success` means reviewed or exempt; the merger's hygiene step covers the exempt diff, except a diff that touches the workflow file, which always gets the merger's subagent. It reviews the change, not the old code around it, and posts inline comments for potential bugs, security issues and inconsistencies only; linting is `make lint`'s job.
- Every review comment, from the review agent or a human, gets a reply or a fix before merge. The reviewer's report plus green required checks is not a substitute for checking the comment threads.
- Comment raises something real but out of scope: open a new task, do not merge silently. Never let a flagged issue merge unaddressed and untracked.
- Once addressed (fixed, or filed as a task), resolve the thread on the PR. Addressed-but-unresolved threads are not done.

## 7. Two deployables, one command

- `make dev` runs backend and frontend locally. Backend is a Go binary; frontend is a static build. They are deployed separately and never embedded into each other.
- Deployment configuration (paths, ports, `SECRET_KEY`, `DOMAIN`) comes from environment variables. Install-wide settings an admin changes at runtime (provider keys, schedule, registration) live in the `settings` table, see [ADR 0010](adr/0010-setup-and-admin.md). Sensible defaults for local development.

## 8. Explicit over implicit

- Timestamps in UTC. Market calendar in `Europe/Istanbul`.
- JSON API with stable, documented shapes.
- Errors are returned, logged once at the boundary, never swallowed.
- No task ids in code comments; git blame and the task's `PR` property in Notion carry them. `ADR NNNN` pointers are fine.

## 9. Data sources are replaceable

- Five markets: BIST, US, TEFAS, CRYPTO, GOLD. No shared provider interface: each provider is one package with a concrete fetcher and sync, driven by the daily job as a `scheduler.Source`. See [ADR 0003](adr/0003-multi-currency-and-market-data-sources.md).
- Manual prices are per user. On the same date a user's manual price wins over the fetched one; otherwise the newer row wins.
- Provider quirks stay inside the provider package.

## 10. Roadmap is a guide, not a contract

- Phases in `ROADMAP.md` are ordered by value and dependency. Reorder when learning says so, but update the doc and the Epic tasks in the Tasks database together. A shipped phase leaves this file; its Done epic is the record. An ADR states what holds now; the convention is in `docs/adr/README.md`.
