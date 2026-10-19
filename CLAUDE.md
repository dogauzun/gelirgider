# gelirgider

This file is the maintainers' agent workflow; people start at [CONTRIBUTING.md](CONTRIBUTING.md).

Multi-user portfolio tracking tool. Markets: BIST, US stocks, TEFAS funds, crypto, gold.
Development is done mostly by AI agents. Read this file fully before any work.

## Repo layout (monorepo)

```
backend/    Go module (JSON API server); cmd/symbols refreshes internal/instrument/*_symbols.csv, cmd/statement_dump dumps broker PDFs
frontend/   React + TypeScript + Vite
deploy/     Dockerfiles, compose, Caddyfile, nginx.conf, backup.sh, README, RUNBOOK
docs/       ROADMAP.md, PRINCIPLES.md, ADRs (adr/)
.claude/    agents, skills, routines (the full instructions of the five cloud routines)
Makefile    single entrypoint: make setup / dev / test / lint / build / swagger
```

## Tech stack

- Backend: Go (latest stable), `net/http` stdlib mux, `database/sql`, `shopspring/decimal` for money. No web framework, no ORM.
- Database: SQLite (`modernc.org/sqlite`, no cgo), also in production (ADR 0002). Write portable SQL, keep all SQL behind a small repository layer, no dialect-only features, so a later move stays cheap. Schema changes are plain `.sql` files applied in order, forward-only, no migration tool (ADR 0002).
- Frontend: React + TypeScript + Vite, TanStack Query, react-router, Tailwind, Base UI for menus, selects, popovers, checkboxes and autocomplete (the current list is in ADR 0023), Recharts for charts. Talks to backend over JSON REST at `/api/*`. Semantic color and shadow tokens in index.css, light and dark; no raw palette or shadow classes in components (ADR 0023).
- Prod: backend and frontend are separate deployables. Backend Docker image; frontend static build served by a web server or CDN. Frontend is never embedded in the Go binary.
- CI: GitHub Actions runs on every pull request and on `main` (`.github/workflows/checks.yml`, `.github/workflows/pr-title.yml`). The five checks (`Go tests`, `Web tests`, `Web build`, `Lint (Go + web)` in workflow `ci`, `Title` in workflow `pr`) are required on `main`, and a PR merges, through auto-merge, as soon as they are green on its current head; the branch need not be up to date with `main`; what each one runs is in `docs/PRINCIPLES.md` #6. The Actions policy allows GitHub-owned actions only (`actions/*`, `github/*`): never add a third-party action. Code review comes from a separate review agent on Opus 5.5 at high effort that the merger spawns as a subagent as soon as the PR is open, alongside the checks (`.claude/agents/pr-reviewer.md`, ADR 0022; a diff that touches only the exempt paths listed there skips it): it reviews only the changes and posts inline comments for high-value findings (potential bugs, security issues, inconsistencies), no review body and no verdict.

## Workflow rules

1. Work is tracked in Jira project `GLG` (stockpuppet.atlassian.net, kanban board "gelirgider"). Plan and create tickets (or a GitHub issue for outside contributors) before implementation begins. No ticket, no code.
2. Tickets are small and independent. Minimize dependencies so many tickets can run in parallel. Short but never missing important context.
3. No story points, no sprints. Kanban only. A ticket that looks like more than a day of work gets split. A mechanical change that one reviewer would read as one unit (a rename, removing one comment pattern, clearing one lint exception) is one ticket and one PR, however many files it touches; small tickets are for logic, not for file count.
4. Problem found that is unrelated to current ticket: create a new GLG ticket. Do not fix it in the current PR.
5. One PR per ticket. Branch name `GLG-<n>-short-slug`.
6. Address every review comment and every failing required check on the PR head before merge, and resolve each addressed thread; an out-of-scope finding becomes a new GLG ticket (rule 4). See `docs/PRINCIPLES.md` #6.
7. Once the review agent has reviewed a PR's current diff (or the diff touches only the exempt paths listed in ADR 0022 and needs no review) and rule 6 is satisfied, the merger turns on auto-merge (`gh pr merge --auto --squash --subject "<PR title> (#<n>)" --body "<the PR's Description text>"`) and GitHub merges the PR as soon as the required checks are green on its head; no rebase onto `main` is needed first. Always pass `--subject` and `--body`, never through a tool that cannot (the desktop app's `set_auto_merge`). After GitHub merges the PR, the merger reads that PR's own squash commit back, not the tip of `main` (auto-merges land asynchronously), with `git fetch origin main && git log -1 --format=%B "$(gh pr view <n> --json mergeCommit --jq .mergeCommit.oid)"`, and reports a trailer. A push run on `main` that fails after a merge is a bug ticket for the merger that landed it. Auto-merge is turned on only as the last step, after the reviewer has reviewed the current head (or the diff is exempt) and every thread is resolved; before any push that changes the diff after that, the merger runs `gh pr merge --disable-auto` and turns auto-merge on again only after the new review (or the diff is still exempt). The merger spawns the reviewer (`pr-reviewer`, never the agent that wrote the code) as soon as the PR is open, in parallel with the required checks, which stay a condition for the merge and not for starting the review, and again after every push that changes the PR's diff and leaves it non-exempt; a rebase onto `main` that resolved no conflict, or a finding answered without a push, needs no new review. The merger also checks that non-trivial logic ships with a test, that a bug fix's Testing section names a test that failed before the fix (or says why no test can express the bug), and that a new or changed route has its swag annotation. After six review rounds with findings the ticket is parked with `needs-decision` (ADR 0022).
8. The ponytail ruleset is `docs/PRINCIPLES.md` #1 and binds every agent, local or cloud. The `ponytail` and `caveman` plugins are optional conveniences of a local session (user scope); nothing in the repo depends on them.
9. No AI attribution anywhere: no `Co-Authored-By` trailers, no "Generated with Claude Code" lines in commits, PRs, or files, and no commit carries an AI author or committer identity either (a sandbox default such as `Claude <noreply@anthropic.com>` becomes a `Co-authored-by` trailer on `main` under the repository's squash default); check `git log origin/main..HEAD --format='%an <%ae> %cn <%ce>'` before pushing.
10. One ticket, one agent session. Never work two tickets in one session. Transition to `In Progress` when work starts, `In Review` when the PR opens. Stay on the ticket until the PR merges, then transition to `Done`. Jira transition timestamps are the effort record; nothing is estimated. The ticket is implemented by the `ticket-implementer` agent on Sonnet 5.5 at high effort (`.claude/agents/ticket-implementer.md`); a harness that cannot load that agent type spawns a general subagent with that model and effort and tells it to read the file.
11. Releases: run the `release` workflow in Actions; the version comes from the squash titles since the last tag, the notes are GitHub's generated notes.

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
- Bug fix: write the test first and see it fail; the PR's Testing section names it (`docs/PRINCIPLES.md` #6).
- New or changed API route: update its swag annotations; run `make swagger` and commit `backend/apidocs/`; `make lint` fails on a stale spec (see ADR 0013).
