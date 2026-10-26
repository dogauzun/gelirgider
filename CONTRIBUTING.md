# Contributing

Thanks for helping. This file is the entry point for people; [CLAUDE.md](CLAUDE.md)
is the maintainers' agent workflow. Security problems go through
[SECURITY.md](SECURITY.md), not a public issue.

## How the project is run

The maintainers develop gelirgider mostly with AI agents, against a private
task database in Notion: every change starts as a task and ships as one pull
request, reviewed by a separate review agent (the `Claude Code Review`
workflow, or for a pull request from a fork the same review agent run by a
maintainer; ADR 0037) before it is merged. You do not need access to that
database. Open a GitHub issue instead of a task, and name that issue in the
body of your pull request (`Closes #<n>`).

## Running it

Prerequisites, provider API keys and first-run notes are in the
[README](README.md#development). From the repository root:

```
make setup               # install frontend dependencies (reuses the main checkout's when the lockfile matches)
make dev                 # backend on :8080 and frontend on :5173, Ctrl+C stops both
make test                # backend and frontend tests
make lint                # linters and format checks
make build-frontend      # type-check and production build of the frontend
```

GitHub Actions runs the same targets on every pull request as the checks
`Go lint`, `Web lint`, `Go tests`, `Web tests` and `Web build`, plus `Title` for the
pull request title, and a pull request merges only when they pass. Still run
`make lint`, `make test` and `make build-frontend` before you push.
[docs/PRINCIPLES.md](docs/PRINCIPLES.md) #6 lists exactly what each one runs.

## Pull requests

One concern per pull request. The title follows Conventional Commits:
`<type>: <what changed>`, lowercase imperative after the colon, for example
`fix: keep the allocation donut's tooltip inside the donut`, with no suffix.
Types: `feat` new behavior, `fix` wrong behavior, `perf`, `refactor`, `test`,
`docs`, `build` (Makefile, Docker, dependencies), `chore` (anything else).

The body is free text: what changed and why, how you verified it (the
commands you ran and what you saw), and `Closes #<n>` for the issue.

Releases: run the `release` workflow in Actions; the version comes from the squash
titles since the last tag, the notes are GitHub's generated notes.

## Rules the review checks

- Bug fix: write the test first and see it fail, and name it in the Testing
  section ([docs/PRINCIPLES.md](docs/PRINCIPLES.md) #6).
- Non-trivial logic ships with one test. Trivial code ships with none.
- New or changed API route: update its swag annotations, run `make swagger` and
  commit `backend/apidocs/`. `make lint` fails on a stale spec.
- UI strings go through i18n keys. A new key goes into the area file of its
  prefix, `frontend/src/i18n/tr/<area>.ts` and `frontend/src/i18n/en/<area>.ts`,
  with a `tr` (default) and an `en` entry in the same change.
- Money and quantities are `decimal`, never `float`. Every query on user data
  filters by `user_id`.
- Code, comments, commits and pull requests are in English.
- A change to `README.md` is mirrored in its Turkish translation `README.tr.md`
  in the same pull request.

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) has the full set of principles and
[docs/adr/](docs/adr/) the decisions behind them.
