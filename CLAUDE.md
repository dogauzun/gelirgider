# gelirgider

This file is the maintainers' agent workflow; people start at [CONTRIBUTING.md](CONTRIBUTING.md).

Multi-user portfolio tracking tool. Markets: BIST, US stocks, TEFAS funds, crypto, gold.
Development is done mostly by AI agents. Read this file fully before any work.

## Repo layout (monorepo)

```
backend/    Go module (JSON API server); cmd/symbols refreshes internal/instrument/*_symbols.csv, cmd/statement_dump dumps broker PDFs
frontend/   React + TypeScript + Vite
deploy/     Dockerfiles, compose, Caddyfile, nginx.conf, backup.sh, README, RUNBOOK
docs/       ROADMAP.md, HISTORY.md, PRINCIPLES.md, EFFORT.md, ADRs (adr/), phase specs and plans (superpowers/)
Makefile    single entrypoint: make dev / test / lint / build / swagger
```

## Tech stack

- Backend: Go (latest stable), `net/http` stdlib mux, `database/sql`, `shopspring/decimal` for money. No web framework, no ORM.
- Database: SQLite (`modernc.org/sqlite`, no cgo), also in production (ADR 0002). Write portable SQL, keep all SQL behind a small repository layer, no dialect-only features, so a later move stays cheap. Schema changes are plain `.sql` files applied in order, forward-only, no migration tool (ADR 0002).
- Frontend: React + TypeScript + Vite, TanStack Query, react-router, Tailwind, Base UI for menus, selects, popovers, checkboxes and autocomplete (the current list is in ADR 0023), Recharts for charts. Talks to backend over JSON REST at `/api/*`. Semantic color and shadow tokens in index.css, light and dark; no raw palette or shadow classes in components (ADR 0023, which supersedes parts of ADR 0005).
- Prod: backend and frontend are separate deployables. Backend Docker image; frontend static build served by a web server or CDN. Frontend is never embedded in the Go binary.
- CI: GitHub Actions runs `make lint`, `make test-backend`, `make test-frontend` and `make build-frontend` as separate jobs on every pull request and on `main` (`.github/workflows/checks.yml`; on a pull request the Go and web test and build jobs skip their work when their side did not change) and checks the PR title (`.github/workflows/pr-title.yml`). The five checks (`Go tests`, `Web tests`, `Web build`, `Lint (Go + web)` in workflow `ci`, `Title` in workflow `pr`) are required on `main`, and a PR merges only when they are green on its current head with the branch up to date; what each target runs is in `docs/PRINCIPLES.md` #6. The Actions policy allows GitHub-owned actions only (`actions/*`, `github/*`): never add a third-party action. Code review comes from a separate review agent on Opus 5.5 at high effort that the merger spawns as a subagent (`.claude/agents/pr-reviewer.md`, ADR 0022): it reviews only the changes and posts inline comments for high-value findings (potential bugs, security issues, inconsistencies), no review body and no verdict.

## Workflow rules

1. Work is tracked in Jira project `GLG` (stockpuppet.atlassian.net, kanban board "gelirgider"). Plan and create tickets (or a GitHub issue for outside contributors) before implementation begins. No ticket, no code.
2. Tickets are small and independent. Minimize dependencies so many tickets can run in parallel. Short but never missing important context.
3. No story points, no sprints. Kanban only. A ticket that looks like more than a day of work gets split.
4. Problem found that is unrelated to current ticket: create a new GLG ticket. Do not fix it in the current PR.
5. One PR per ticket. Branch name `GLG-<n>-short-slug`.
6. Address every review comment and every failing required check on the PR head before merge. This is not optional and not covered by the reviewer's report alone — check comment threads explicitly. Out-of-scope issue found in a comment: open a new GLG ticket (see rule 4), do not merge it unaddressed and untracked. Once a comment thread is addressed (fixed, or ticketed per rule 4), resolve it on the PR — do not leave addressed threads open. See `docs/PRINCIPLES.md` #6.
7. A PR may be merged automatically once the review agent has reviewed its current diff, the required checks are green on the PR head, and rule 6 is satisfied. The merger spawns the reviewer (`pr-reviewer`, never the agent that wrote the code) when the PR is open and its required checks are green, and again after every push that changes the PR's diff; a rebase onto `main` that resolved no conflict, or a finding answered without a push, needs no new review. The merger also checks that non-trivial logic ships with a test, that a bug fix's Testing section names a test that failed before the fix (or says why no test can express the bug), and that a new or changed route has its swag annotation. After six review rounds with findings the ticket is parked with `needs-decision` (ADR 0022).
8. Ensure `caveman` and `ponytail` plugins are active before starting development.
9. No AI attribution anywhere: no `Co-Authored-By` trailers, no "Generated with Claude Code" lines in commits, PRs, or files.
10. One ticket, one agent session. Never work two tickets in one session. Transition to `In Progress` when work starts, `In Review` when the PR opens. Stay on the ticket until the PR merges, then transition to `Done`. See `docs/EFFORT.md`. The ticket is implemented by the `ticket-implementer` agent on Sonnet 5.5 at high effort (`.claude/agents/ticket-implementer.md`); a harness that cannot load that agent type spawns a general subagent with that model and effort and tells it to read the file.

## PR format

Title: Conventional Commits, `<type>: <what changed> (GLG-<n>)`, lowercase imperative after the colon, e.g. `fix: keep the allocation donut's tooltip inside the donut (GLG-1028)`. Without a GLG ticket (a GitHub issue for outside contributors), drop the `(GLG-<n>)` suffix. Types: `feat` new behavior, `fix` wrong behavior (default for a Bug ticket), `perf`, `refactor`, `test`, `docs`, `build` (Makefile, Docker, dependencies), `chore` (anything else). The squash-merge subject is the PR title plus ` (#<pr>)`.

Body:

```
## Description
A short description of what is done

Ticket: <link to GLG ticket, or a GitHub issue for outside contributors>

## Testing
Very short explanation of how testing is conducted to verify the PR. Include what commands are run and what is observed
```

## Coding rules (short form, see docs/PRINCIPLES.md)

- Laziest working solution. Stdlib before dependency. No speculative abstraction.
- Every table that holds user data has `user_id`. Every query filters by it. No exceptions.
- Money and quantities are `decimal`, never `float`. Stored as exact text or numeric, never as a float column.
- Store timestamps in UTC. Market dates are `DATE` in `Europe/Istanbul`.
- Code, comments, commits, tickets, PRs in English. UI strings go through i18n keys from day one; locales `tr` (default) and `en`.
- Non-trivial logic ships with one test. Trivial code ships with none.
- Bug fix: write the test first. Turn the bug report into a test that fails for the reported reason, run it and see it fail, then fix until it passes. The PR's Testing section names the test and says it failed before the fix. No test can express the bug (pure layout, a docs fix): say why in the PR.
- New or changed API route: update its swag annotations; run `make swagger` and commit `backend/apidocs/`; `make lint` fails on a stale spec (see ADR 0013).
