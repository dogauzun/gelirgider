---
name: qa-run
description: Use when running a local QA pass of gelirgider by hand or as a QA subagent: starting an isolated stack, creating users, driving the browser, and knowing which behaviors are deliberate and must not be filed again.
---

# QA run

How to bring up a local stack and exercise it. QA findings that are bugs become tasks (`.claude/rules/tracker.md`); the behaviors in "Deliberate behaviors" do not.

## Local stack

- Backend: `DB_PATH=<scratch>/qa.db PORT=8098 go run ./cmd/server` from `backend/`, or a binary built into the scratchpad.
- Frontend: `npm run dev` in `frontend/` on 5173 (its `/api` proxy follows the `PORT` variable), or a Vite temp config on a spare port.
- Ports 8080, 8099 and 5173 may hold a stale `go run` or Vite from another session. Before killing one, look at it: `lsof -t -iTCP:<port>` and `ps -o comm= -p <pid>`. After starting yours, check again who holds the port: a bind failure is silent in a backgrounded start, and a rebuilt server exits silently when the port is taken.
- Never stop servers with `pkill -f`: it kills the calling shell (exit 144). Write a pid file when you start one and kill by pid.

## Fresh database

- The startup catch-up fetches real BIST prices unless the sources are switched off first. Switch them off with `PUT /api/admin/sources/{name}`, or with rows in the `settings` table before the first start, as `make screenshots` does.
- First admin: the setup token is in the server log (`setup_token`); `POST /api/setup/admin` creates the admin. Further users: `POST /api/auth/register`, then `POST /api/auth/login`. In a curl flow register alone does not set the cookie.

## Browser

- The in-app browser pane has quirks: the Return key and `window.confirm` do not work. Fill forms with form input and click the buttons. Test a confirm-gated delete through curl.
- Overflow check: compare `document.documentElement.scrollWidth` with the emulated width, never `innerWidth`.
- Headless Chrome through CDP beats the browser pane for chart QA and screenshots.
- PDF upload: build a synthetic Midas or Garanti PDF and hand it to the file input through the page, or serve it from a CORS-enabled file server and `fetch` it into a `File`.

## Parallel QA

- One isolated stack per agent: its own DB and its own ports. Split the areas by page.
- Subagents share the orchestrator's scratchpad, so every scratch file starts with the task's ID.

## Fetchers

QA a fetcher against a real downloaded sample, not only its fixtures: a parser bug once hid behind a fixture that repeated one header. FX rates can be seeded by hand.

## Deliberate behaviors, not bugs

Do not file these.

- Selling more than held and withdrawing more than the balance are accepted; the UI shows a "Negatif" pill ([ADR 0019](../../../docs/adr/0019-core-engine-and-auth.md)).
- Market value equals cost basis while no price is known ([ADR 0016](../../../docs/adr/0016-position-detail.md)).
- `DELETE` of a portfolio with `?force=true` cascades to its transactions.
- The history range is clamped to 3660 days ([ADR 0019](../../../docs/adr/0019-core-engine-and-auth.md)).
- The login rate limit is per client IP ([ADR 0019](../../../docs/adr/0019-core-engine-and-auth.md)).
- Turkish "10.000" style numbers are accepted.
- The one-time setup token is logged at WARN ([ADR 0010](../../../docs/adr/0010-setup-and-admin.md)).
- No CSRF token, by design: `SameSite=Lax` plus the cross-origin check on mutating requests ([ADR 0019](../../../docs/adr/0019-core-engine-and-auth.md)).
- Request bodies are capped at 64 KB except statement import ([ADR 0019](../../../docs/adr/0019-core-engine-and-auth.md)).
- Returns are null on a non-positive base ([ADR 0011](../../../docs/adr/0011-portfolio-returns.md)).
- NFD-normalized names and dialog performance at thousands of rows are won't-do.
- The Midas zero-commission row is skipped, not booked as a deposit ([ADR 0006](../../../docs/adr/0006-broker-statement-import.md)).

## Not verified yet

- The 8 MB decoded-stream cap on a PDF, against real statements.
- The 0.3 s history target.
- The delete path of `.github/workflows/prune-images.yml` has never run against real versions. A 403 on a user-owned package would need an owner PAT, and no agent creates one.
