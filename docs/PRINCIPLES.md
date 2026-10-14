# Guiding principles

## 1. Ship the smallest thing that works

- Ask first: does this need to exist? If speculative, skip and say so.
- Reuse what is already in the repo. Then stdlib. Then an installed dependency. New dependency last.
- No interface with one implementation. No config for a value that never changes. No scaffolding "for later".
- Deletion beats addition. Boring beats clever.

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

- One ticket, one PR, one concern. Vertical slices over horizontal layers when possible.
- Tickets are written so they can be worked in parallel. Name the dependency if one truly exists.
- Unrelated problem found mid-ticket: open a new ticket. Never widen the PR.

## 6. Verified and reviewed before merged

- The required GitHub Actions checks green on the PR head is the bar: `Lint (Go + web)`, `Go tests`, `Web tests` and `Web build` run the Make targets `lint`, `test-backend`, `test-frontend` and `build-frontend` (`.github/workflows/checks.yml`, workflow `ci`, listed below), `Title` checks the title (workflow `pr`, `.github/workflows/pr-title.yml`). On a pull request the checks are change-scoped: `Go tests` does its work only when `backend/` changed, `Web tests` and `Web build` only when `frontend/` changed, a `Makefile` or `.github/workflows/` change runs all of them, `Lint (Go + web)` always runs, and a skipped check still reports green; pushes to `main` run everything. The author still runs `make lint`, `make test` and `make build-frontend` from the repo root before pushing.
  - `make lint`: `make swagger-check`, `gofmt -l backend` (fails if it prints a file), `go vet ./...`, `golangci-lint run` (`dupl`, `forbidigo`, `funlen` with `lines` 150 (comments ignored, tests exempt), `gocognit` with `min-complexity` 45, `nestif` with `min-complexity` 10, `noctx`, `revive` with `argument-limit` 6 and `usestdlibvars` with `time-layout`, configured in `backend/.golangci.yml`) and `deadcode ./...` (without `-test`, fails if it prints a function) in `backend/`, `npm run lint`, `npm run lint:dupl` (`jscpd`, configured in `frontend/.jscpd.json`), `npm run knip` (unused exports and types, configured in `frontend/knip.json`), `npx prettier --check .` in `frontend/`, prettier check on `docs/`, `deploy/`, `.claude/` and the root `*.md` files.
  - `make test`: `make test-backend` (`go vet ./...` and `go test ./...` in `backend/`) and `make test-frontend` (`npm run lint` and `npm test` in `frontend/`). CI runs the two as separate jobs.
  - `make build-frontend`: `npm run build` in `frontend/`. Neither `make lint` nor `make test` runs it.
- Non-trivial logic ships with a test that fails when the logic breaks. One test, not a suite.
- A bug fix starts with the test. Before writing the fix, turn the bug report into a test that fails for the reported reason, run it and watch it fail. The fix is done when that test passes and the others stay green. The PR's "Testing" section names the test and states that it failed before the fix.
- When no automated test can express the bug (pure visual layout, a documentation fix), say so in the PR and describe the manual check instead.
- PR "Testing" section states the commands run and what was observed.
- Every PR's current diff is reviewed by the review agent the merger spawns (`.claude/agents/pr-reviewer.md`, [ADR 0022](adr/0022-pr-review-agent.md)). It reviews the change, not the old code around it, and posts inline comments for potential bugs, security issues and inconsistencies only; linting is `make lint`'s job.
- Every review comment, from the review agent or a human, gets a reply or a fix before merge. The reviewer's report plus green required checks is not a substitute for checking the comment threads.
- Comment raises something real but out of scope: open a new GLG ticket, do not merge silently. Never let a flagged issue merge unaddressed and untracked.
- Once addressed (fixed, or ticketed), resolve the thread on the PR. Addressed-but-unresolved threads are not done.

## 7. Two deployables, one command

- `make dev` runs backend and frontend locally. Backend is a Go binary; frontend is a static build. They are deployed separately and never embedded into each other.
- Deployment configuration (paths, ports, `SECRET_KEY`, `DOMAIN`) comes from environment variables. Install-wide settings an admin changes at runtime (provider keys, schedule, registration) live in the `settings` table, see [ADR 0010](adr/0010-setup-and-admin.md). Sensible defaults for local development.

## 8. Explicit over implicit

- Timestamps in UTC. Market calendar in `Europe/Istanbul`.
- JSON API with stable, documented shapes.
- Errors are returned, logged once at the boundary, never swallowed.

## 9. Data sources are replaceable

- Five markets: BIST, US, TEFAS, CRYPTO, GOLD. No shared provider interface: each provider is one package with a concrete fetcher and sync, driven by the daily job as a `scheduler.Source`. See [ADR 0003](adr/0003-multi-currency-and-market-data-sources.md).
- Manual prices are per user. On the same date a user's manual price wins over the fetched one; otherwise the newer row wins.
- Provider quirks stay inside the provider package.

## 10. Roadmap is a guide, not a contract

- Phases in `ROADMAP.md` are ordered by value and dependency. Reorder when learning says so, but update the doc and the Jira epics together. Shipped phases move to `HISTORY.md`.
