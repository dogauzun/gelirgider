# ADR 0019: Core engine and auth

Status: accepted (2026-10-01).

## Context

Deliberate behaviors of the holdings engine and of auth keep being filed as
bugs, negative positions and cash overdraft first among them. The code is under
`backend/internal/` (`auth`, `holdings`, `transaction`, `server`).

## Decision

### Auth and sessions

- **Passwords are bcrypt hashes** at the default cost. A password is 8
  characters to 72 bytes (bcrypt's own input limit), may not be only
  whitespace and is never trimmed. A login with an unknown email compares
  against a dummy hash, so response time does not reveal whether an email is
  registered.
- **Sessions are server-side rows behind a cookie.** The cookie is `session`,
  `HttpOnly`, `SameSite=Lax`, and `Secure` when `COOKIE_SECURE=true`. A session
  lasts 30 days from login and is never extended. The table stores only the
  sha256 of the token, so a database copy or a `-backup` file carries no
  usable login. Expired rows are deleted on every login and once at startup.
- **Two-step login is optional**, a TOTP code after the password, see
  [ADR 0025](0025-two-step-login.md).
- **One `RequireUser` wrap** guards every `/api/*` route except the public
  auth, setup and health ones. `RequireAdmin` reads `is_admin` per request.
- **A password change signs out every other session** but the current one.
- **Auth rate limit: 10 requests per minute per client IP**, hardcoded, on
  register, login and its TOTP step, the password change, the two-step login
  setup, confirm, disable and recovery-code routes
  ([ADR 0025](0025-two-step-login.md)), and first-time setup. The limiter is an
  in-memory fixed window per route, so it holds for a single backend instance
  only. The client IP depends on `TRUST_PROXY`
  ([ADR 0020](0020-deployment-topology.md)).
- **Cross-origin check on mutating requests.** The handler sits behind the
  stdlib `http.CrossOriginProtection`: a POST, PUT, PATCH or DELETE from another
  origin gets 403, which `SameSite=Lax` alone does not give for a sibling
  subdomain.
- **Request bodies are capped at 64 KB**, except the statement import routes,
  which set their own limit ([ADR 0006](0006-broker-statement-import.md)).
- **No self-service reset**, since sending mail is not part of the product. The
  operator runs `-set-password <email>` (new password on stdin) or an admin uses
  the admin API ([ADR 0010](0010-setup-and-admin.md)); both end that user's
  sessions.

### Holdings engine

- **Holdings are derived, never stored.** Positions and cash are computed from
  the transaction rows on each request, by `holdings`, which has no database
  and no HTTP.
- **Weighted average cost, not FIFO.** A buy adds `amount + fee` to the cost
  basis. A sell realizes `amount - fee - quantity × avg_cost` and leaves the
  average cost unchanged. A position that reaches quantity 0 resets its cost
  basis and average cost. A buy into a negative position first covers the
  oversold quantity and books that part as realized P/L.
- **Negative positions are allowed on purpose.** No layer checks that a sell is
  covered by holdings; the UI shows a "Negatif" pill. A sell with no position
  realizes its whole net proceeds.
- **Cash overdraft is allowed on purpose.** Cash is kept per currency with no
  implicit conversion ([ADR 0003](0003-multi-currency-and-market-data-sources.md))
  and has no floor. A buy with no deposit behind it leaves negative cash, which
  the value history flags, as there is no base to measure a return on.
- **Same-day ordering** is date, then splits, then id. A sell that would take
  its instrument below zero has the same day's later buys moved ahead of it,
  because a newest-first broker history enters a day trade sell-first. Ordering
  stays inside each portfolio ([ADR 0014](0014-all-portfolios-view.md)).

### Prices and history

- **Value history is computed on request** from the transactions and the price
  history; there is no snapshot table or job. A range is at most 3660 days.
- **A manual price is per user** (`manual_prices`), never written into the
  shared `prices` table. It wins over a fetched price on the same date only;
  otherwise the newer row wins.
- **Saving a transaction fetches what it needs.** A create or update of a
  transaction with an instrument, or of cash in a currency other than TRY,
  fetches the missing price synchronously with a 5 second timeout (errors are
  logged and ignored) and queues a background backfill from the transaction
  date. One worker runs one walk at a time and merges requests that arrive
  meanwhile per source. A weekday with no data is remembered once a later date
  in the walk is synced, so holidays are not refetched on every restart.
