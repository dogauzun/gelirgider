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
.claude/    agents, rules, skills
Makefile    single entrypoint: make setup / dev / test / lint / build / swagger
```

## Tech stack

- Backend: Go (latest stable), `net/http` stdlib mux, `database/sql`, `shopspring/decimal` for money. No web framework, no ORM.
- Database: SQLite (`modernc.org/sqlite`, no cgo), also in production (ADR 0002). Write portable SQL, keep all SQL behind a small repository layer, no dialect-only features, so a later move stays cheap. Schema changes are plain `.sql` files applied in order, forward-only, no migration tool (ADR 0002).
- Frontend: React + TypeScript + Vite, TanStack Query, react-router, Tailwind, Base UI for menus, selects, popovers, checkboxes and autocomplete (the current list is in ADR 0023), Recharts for charts. Talks to backend over JSON REST at `/api/*`. Semantic color and shadow tokens in index.css, light and dark; no raw palette or shadow classes in components (ADR 0023).
- Prod: backend and frontend are separate deployables. Backend Docker image; frontend static build served by a web server or CDN. Frontend is never embedded in the Go binary.
- CI: GitHub Actions runs on every pull request and on `main` (`.github/workflows/checks.yml`, `.github/workflows/pr-title.yml`, `.github/workflows/claude-code-review.yml`). The seven checks (`Go lint`, `Web lint`, `Go tests`, `Web tests`, `Web build` in workflow `ci`, `Title` in workflow `pr`, `Claude review` in workflow `Claude Code Review`) are required on `main`, and a PR merges, through auto-merge, as soon as they are green on its current head; the branch need not be up to date with `main`; what each one runs is in `docs/PRINCIPLES.md` #6. The Actions policy allows GitHub-owned actions (`actions/*`, `github/*`) and the two patterns the review workflow needs, `anthropics/claude-code-action@*` and `oven-sh/setup-bun@*` (the action's one nested step): never add another third-party action. Code review comes from a separate review agent on Opus 5.5 at high effort that the workflow `Claude Code Review` (`.github/workflows/claude-code-review.yml`, ADR 0037) runs on every push to a PR of this repository, alongside the checks, from `.claude/agents/pr-reviewer.md` (finding rules: ADR 0022; the exempt list lives only in the workflow's `scope` step, and for an exempt diff the job `Claude review` ends green without a review, with a job summary line saying so; it also ends green without a review for a fork PR, a Dependabot PR and a PR that changes the workflow file, which the merger reviews with a subagent): it reviews only the changes and posts inline comments for high-value findings (potential bugs, security issues, inconsistencies), no review body and no verdict. `Claude review` is a required check, so a PR waits for the review of its current head; if the run fails, it is re-run until it succeeds, and an outage that lasts goes to the owner (ADR 0037).

## Workflow rules

1. Work is tracked in the gelirgider Tasks database in Notion, on the page "Projects / gelirgider" (database ids, status and label names, house style and SQL recipes: `.claude/rules/tracker.md`; the decision: ADR 0040). A task is a page there. Plan and create tasks (or a GitHub issue for outside contributors) before implementation begins. The owner may decide that a change runs without a task; the PR's Description then says so and why. Otherwise: no task, no code.
2. Tasks are small and independent. Minimize dependencies so many tasks can run in parallel. Short but never missing important context. Every dependency between tasks is a `Blocked by` relation on the blocked task (`Blocks` is its reverse) besides the "Depends on" text in the description.
3. No story points, no sprints. Kanban only. A task that looks like more than a day of work gets split. A mechanical change that one reviewer would read as one unit (a rename, removing one comment pattern, clearing one lint exception) is one task and one PR, however many files it touches; small tasks are for logic, not for file count.
4. Problem found that is unrelated to current task: create a new task. Do not fix it in the current PR.
5. One PR per task. Branch name `short-slug`, never a task key.
6. Address every review comment and every failing required check on the PR head before merge, and resolve each addressed thread; an out-of-scope finding becomes a new task (rule 4). See `docs/PRINCIPLES.md` #6.
7. Once `Claude Code Review` has concluded `success` on a PR's current head and the diff does not touch `.github/workflows/claude-code-review.yml` (`success` means reviewed or exempt, and the run's job summary says which; or the merger's subagent has reviewed the current head: a fork PR, a Dependabot PR, a PR that changes the workflow file even when the run reports it exempt, or the owner has lifted the required check for an outage) and rule 6 is satisfied, the merger turns on auto-merge (`gh pr merge --auto --squash --subject "<PR title> (#<n>)" --body "<the PR's Description text>"`) without asking the owner, and GitHub merges the PR as soon as the seven required checks are green on its head; no rebase onto `main` is needed first. The merger asks the owner only at the decisions rule 12 lists; a merge conflict is synced with `main`, not asked. Always pass `--subject` and `--body`, never the desktop app's `set_auto_merge`, which takes neither. The body is the PR's Description text; extract it with python, not BSD `sed`, refuse an empty body, and keep the literal `Co-Authored-By` trailer string out of a Description (reword it), because it would become a trailer on `main`. After GitHub merges the PR, the merger reads that PR's own squash commit back, not the tip of `main` (auto-merges land asynchronously), with `git fetch origin main && git log -1 --format=%B "$(gh pr view <n> --json mergeCommit --jq .mergeCommit.oid)"`, and reports a trailer. GitHub can take several minutes to merge even when every check is green and the merge state is clean: the merger waits for it and never falls back to a direct `gh pr merge --squash`. A PR whose head predates a required-check change sits blocked with every visible check green, because the new checks never run on it: the merger runs `gh pr merge <n> --disable-auto`, `gh pr update-branch <n>` (the one use of the update-branch endpoint that ADR 0022 allows), waits for the review run on the new head, then turns auto-merge on again. A push run on `main` that fails after a merge is a bug task for the merger that landed it. Auto-merge is turned on only as the last step, after the review run has concluded on the current head and every thread is resolved; before any push that changes the diff after that, the merger runs `gh pr merge --disable-auto` and turns auto-merge on again only after the new head is reviewed (the run concluded with a review or exempt, or the merger's subagent reviewed it where this rule requires that). The workflow starts the review on every push by itself and cancels the run of a stale head, so the merger spawns nothing for a PR of this repository, matches no paths itself and only reads the run (`gh run list --workflow claude-code-review.yml --commit <head SHA>`; every head has one, so a head with no run after a few minutes is a broken run, unless it has a merge conflict, which starts no `pull_request` run: check mergeability first and sync with `main`); a run that failed is re-run until it succeeds, and when an outage lasts, the task is parked with `needs-decision` for the owner, who may lift the requirement for the outage (ADR 0037); for a PR from a fork, a Dependabot PR or a PR that changes `.github/workflows/claude-code-review.yml` (the check passes without a review: the job is skipped, or the action skips a workflow file that differs from `main`'s and still reports `success`), the merger spawns `pr-reviewer` (never the agent that wrote the code) as a subagent for the current head, and does the same for every head while the owner has lifted the required check for an outage; a finding answered without a push needs no new review. The merger also checks that non-trivial logic ships with a test, that a bug fix's Testing section names a test that failed before the fix (or says why no test can express the bug), and that a new or changed route has its swag annotation. After six review rounds with findings the task is parked with `needs-decision` (ADR 0022).
8. The ponytail ruleset is `docs/PRINCIPLES.md` #1 and binds every agent. The `ponytail` and `caveman` plugins are optional conveniences of a local session (user scope); nothing in the repo depends on them.
9. No AI attribution anywhere: no `Co-Authored-By` trailers, no "Generated with Claude Code" lines in commits, PRs, or files, and no commit carries an AI author or committer identity either (a sandbox default such as `Claude <noreply@anthropic.com>` becomes a `Co-authored-by` trailer on `main` under the repository's squash default); check `git log origin/main..HEAD --format='%an <%ae> %cn <%ce>'` before pushing.
10. One task, one implementing session: an implementer never works two tasks in one session and stays on its task until the PR is open; a coordinating session under rule 12 runs several tasks, each in its own implementer. For a PR with a task the coordinating session sets the task's Status to `In Progress` when work starts and `In Review` when the PR opens (and sets the task's `PR` property to the PR URL), stays on it until the PR merges, then sets it to `Done`; the writes go through the Notion MCP (`create-pages`, `update-page`). Nothing is estimated or timed. The task is implemented by the `ticket-implementer` agent on Sonnet 5.5 at high effort (`.claude/agents/ticket-implementer.md`); a harness that cannot load that agent type spawns a general subagent with that model and effort and tells it to read the file.
11. Releases: run the `release` workflow in Actions; the version comes from the squash titles since the last tag, the notes are GitHub's generated notes.
12. A session that coordinates tasks never implements one itself. It spawns one `ticket-implementer` per task whose dependencies are all Done, or a phase-A implementer for a task whose only open blocker decides where its code mounts (`.claude/rules/orchestration.md`), each with `isolation: "worktree"`, in parallel within the machine's capacity, and starts the next batch as dependencies merge without asking the owner. It stops for the owner only at a real decision: a high-risk review finding, a `robots.txt` block, the six-round cap, a review outage that lasts (parked with `needs-decision`), and a failing check that cannot be fixed within the task. It does every tracker write; the implementer writes none.
13. An agent brief carries the task text, the worktree, the branch and file-name hygiene (scratch files start with the task's ID) and nothing else: never a review-waiting, polling or merge-gate step, because the agent file and rule 7 hold the gate. Subagents use no `Monitor` tool and no background watcher (they stall silently) and return when their PR is open (a phase-A agent returns after its local commit and is resumed for phase B).
14. A decision that changes a rule or the environment is an edit to the file that holds the rule (this file, `docs/PRINCIPLES.md`, an ADR, an agent file, `CONTRIBUTING.md`) in the same PR as the change it governs; run `git grep -n -i "<old rule>"` first so no spot keeps the old wording.

## PR format

Title: Conventional Commits, `<type>: <what changed>`, lowercase imperative after the colon, e.g. `fix: keep the allocation donut's tooltip inside the donut`. No task key and no suffix, for anyone. Types: `feat` new behavior, `fix` wrong behavior (default for a Bug task), `perf`, `refactor`, `test`, `docs`, `build` (Makefile, Docker, dependencies), `chore` (anything else). The squash-merge subject is the PR title plus ` (#<pr>)`; the squash body is the Description text.

Body:

```
## Description
A short description of what is done

## Testing
Very short explanation of how testing is conducted to verify the PR. Include what commands are run and what is observed
```

No task link in the body: the tracker is private, and the task's `PR` property (rule 10) is the one link between a task and its PR; the task of a PR is the `PR` lookup recipe in `.claude/rules/tracker.md`. An outside contributor names their GitHub issue in the Description (`Closes #<n>`).

## Coding rules (short form, see docs/PRINCIPLES.md)

- Laziest working solution. Stdlib before dependency. No speculative abstraction.
- Every table that holds user data has `user_id`. Every query filters by it. No exceptions.
- Money and quantities are `decimal`, never `float`. Stored as exact text or numeric, never as a float column.
- Store timestamps in UTC. Market dates are `DATE` in `Europe/Istanbul`.
- Code, comments, commits, tasks, PRs in English. UI strings go through i18n keys from day one; locales `tr` (default) and `en`.
- Non-trivial logic ships with one test. Trivial code ships with none.
- Bug fix: write the test first and see it fail; the PR's Testing section names it (`docs/PRINCIPLES.md` #6).
- New or changed API route: update its swag annotations; run `make swagger` and commit `backend/apidocs/`; `make lint` fails on a stale spec (see ADR 0013).
- A task that adds a new external data host checks `robots.txt` on the real API host first, not the docs or marketing host. A disallow is reported to the owner with manual entry as the fallback and never worked around.

## Memory

Project knowledge lives in this repository: rules here and in `.claude/rules/`, decisions in `docs/adr/` and `.claude/rules/decisions.md`, recipes in `.claude/skills/`. A durable fact learned in a session goes into the file that owns it, in the same PR as the change it came from or in a docs PR of its own. A fact that must not be public (a path on the maintainer's machine, personal data) stays in the maintainer's local auto memory and reaches a subagent only through its brief. Notion holds the task tracker only.
