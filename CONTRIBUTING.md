# Contributing

Thanks for helping. This file is the entry point for people; [CLAUDE.md](CLAUDE.md)
is the maintainers' agent workflow. Security problems go through
[SECURITY.md](SECURITY.md), not a public issue.

## How the project is run

The maintainers develop gelirgider mostly with AI agents, against a private Jira
board (project `GLG`): every change starts as a GLG ticket and ships as one pull
request, reviewed by a separate review agent before it is merged. You do not
need Jira access. Open a GitHub issue instead of a GLG ticket, and name that
issue on the `Ticket:` line of your pull request.

## Running it

Prerequisites, provider API keys and first-run notes are in the
[README](README.md#development). From the repository root:

```
(cd frontend && npm ci)  # install frontend dependencies
make dev                 # backend on :8080 and frontend on :5173, Ctrl+C stops both
make test                # go vet and go test in backend/, npm run lint and npm test in frontend/
make lint                # swag spec check, gofmt, go vet, golangci-lint, deadcode, frontend lint, jscpd, knip, prettier
make build-frontend      # npm run build in frontend/ (type-check and production build)
```

There is no CI. Run `make lint`, `make test` and `make build-frontend` before
you open a pull request; all three must pass. [docs/PRINCIPLES.md](docs/PRINCIPLES.md)
#6 lists exactly what each one runs.

## Pull requests

One concern per pull request. The title follows Conventional Commits:
`<type>: <what changed>`, lowercase imperative after the colon, for example
`fix: keep the allocation donut's tooltip inside the donut`. Maintainers end it
with the ticket key, `(GLG-<n>)`; without a GLG ticket, leave that off.
Types: `feat` new behavior, `fix` wrong behavior, `perf`, `refactor`, `test`,
`docs`, `build` (Makefile, Docker, dependencies), `chore` (anything else).

The body:

```
## Description
A short description of what is done

Ticket: <link to the GitHub issue>

## Testing
Very short explanation of how testing is conducted to verify the PR. Include what commands are run and what is observed
```

## Rules the review checks

- Bug fix: write the test first. Turn the bug report into a test that fails for
  the reported reason, run it and see it fail, then fix until it passes. The
  Testing section names the test and says it failed before the fix. If no test
  can express the bug (pure layout, a docs fix), say why.
- Non-trivial logic ships with one test. Trivial code ships with none.
- New or changed API route: update its swag annotations, run `make swagger` and
  commit `backend/apidocs/`. `make lint` fails on a stale spec.
- UI strings go through i18n keys in `frontend/src/i18n/strings.ts`, with a `tr`
  (default) and an `en` entry in the same change.
- Money and quantities are `decimal`, never `float`. Every query on user data
  filters by `user_id`.
- Code, comments, commits and pull requests are in English.

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) has the full set of principles and
[docs/adr/](docs/adr/) the decisions behind them.
