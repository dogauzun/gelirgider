# ADR 0020: Deployment topology

Status: accepted (GLG-713, 2026-10-01); amended 2026-10-13
(pre-publication review, the private-repository rationale under "No
container registry" is dated)

This ADR records decisions taken in Phase 4 (GLG-41..46,
`docs/HISTORY.md`, Phase 4) that were written down only in the roadmap and in
the `deploy/` files.

## Context

[ADR 0002](0002-production-datastore-and-migrations.md) records the Phase 4
decisions on the datastore, schema files and backups. The rest of the
deployment decisions live only in the Phase 4 section of the roadmap
(`docs/HISTORY.md`, Phase 4), in the non-goals (`docs/ROADMAP.md`, Non-goals) and in
the files under `deploy/`. The roadmap is about to be split, so this ADR
records what is in the repository today, with the reason where a roadmap
line, ticket or commit gives one.

## Decision

### Target: Docker Compose on any Docker host

The deployment target is Docker Compose on any Docker host: a laptop, a
home server or a cloud machine reachable on ports 80 and 443, with no VM
provisioning, no cloud-provider steps and no Kubernetes
(`deploy/RUNBOOK.md:3-5`, `docs/HISTORY.md`, Phase 4). A Kubernetes epic may
come later (`docs/ROADMAP.md`, Non-goals); the reason for deferring it is not
recorded.

### Request path: Caddy, then nginx, then the backend

`deploy/compose.yml` runs three services on one network, `gelirgider`
(`deploy/compose.yml:7-64`):

- `caddy` terminates TLS for `DOMAIN` (`deploy/compose.yml:50-51`) and
  proxies everything to `frontend:80` (`deploy/Caddyfile:1-6`). It obtains
  a Let's Encrypt certificate for a real hostname and a self-signed one for
  `localhost` (`deploy/.env.example:1-6`).
- `frontend` is nginx serving the static build and proxying `/api/` to
  `${BACKEND_UPSTREAM}` (`deploy/nginx.conf:15-16`,
  `deploy/nginx.conf:28-36`), which is substituted at container start
  (`deploy/frontend.Dockerfile:12-16`) and set to `backend:8080` by compose
  (`deploy/compose.yml:35-36`).
- `backend` is the Go binary on port 8080 (`deploy/compose.yml:12-13`).

Only ports 80 and 443 are published, both on `caddy`
(`deploy/compose.yml:47-49`); `frontend` and `backend` are reachable only
from other containers on the compose network (`deploy/README.md:36-38`).
Caddy sits in front of the existing nginx image for TLS
(`docs/HISTORY.md`, Phase 4); why TLS is a separate proxy rather than part of
the nginx image is not recorded.

### The backend behind two proxies

Two environment variables, both `false` by default
(`backend/internal/config/config.go:31-32`), are `true` in compose
(`deploy/compose.yml:15-16`):

- `TRUST_PROXY` makes the backend take the client IP from the first entry
  of `X-Forwarded-For` instead of the connection address
  (`backend/internal/ratelimit/ratelimit.go:84-90`). It is set once at
  startup (`backend/cmd/server/main.go:89`) and used by the rate limiter
  and the request log line (`backend/internal/server/server.go:561`). The
  chain is safe because Caddy is the only service with a published port,
  so it sees the real client and sets `X-Forwarded-For` to that address,
  discarding anything the client sent (`deploy/nginx.conf:5-10`).
- `COOKIE_SECURE` marks the session cookie `Secure`
  (`backend/internal/auth/handlers.go:425-434`). The browser only ever
  talks HTTPS to Caddy (`deploy/README.md:51-53`).

### Content Security Policy

The frontend nginx sets the Content Security Policy and the other security
headers on every response, with `always` (`deploy/nginx.conf:23-26`). They
live in nginx rather than in the Caddyfile so that they also cover the
frontend image run on its own; only HSTS stays in Caddy, the layer that
terminates TLS (`deploy/nginx.conf:18-20`, `deploy/Caddyfile:3-4`).

The policy allows scripts only from files on the same origin
(`script-src 'self'`), so `frontend/index.html` has no inline script
(`deploy/nginx.conf:21-22`). The script that applies the stored theme
before the first paint is therefore a file, `frontend/public/theme.js`,
loaded with a blocking `<script src>` before the stylesheet
(`frontend/index.html:13-15`). A file rather than an inline script lets the
policy keep `script-src 'self'` (`frontend/public/theme.js:1-3`) without a
hash for the inline script (commit 1de6fcb, GLG-473).

### No container registry

The `backend` and `frontend` services have a `build:` section and no
`image:` (`deploy/compose.yml:9-11`, `deploy/compose.yml:32-34`), so
compose builds both images on the Docker host from the git checkout
(`deploy/RUNBOOK.md:79-86`). The reason is recorded in the roadmap: the
repository is private, and a registry would need pull credentials on the
Docker host for no gain (`docs/HISTORY.md`, Phase 4). Only `caddy` uses a
published image, `caddy:2` (`deploy/compose.yml:46`).

> Superseded 2026-10-13 (pre-publication review): the repository is being
> made public, so pull credentials are no longer the reason. The decision stands: images are
> built on the host from the checkout, and a registry would be one more
> moving part for a single-host deployment.

### No external error reporting, logs to stdout

There is no external error reporting service, only panic recovery and
structured logs to stdout (`docs/HISTORY.md`, Phase 4); the reason is not
recorded.

- The backend logs JSON lines to stdout through `slog`
  (`backend/cmd/server/main.go:83`).
- `withRecover` is the outermost middleware; it logs a panic with its stack
  trace and answers with the standard JSON error instead of dropping the
  connection (`backend/internal/server/server.go:575-593`,
  `backend/internal/server/server.go:155`).
- Every service logs through the `json-file` driver, capped at five files
  of 10 MB (`deploy/compose.yml:1-5`, `:27`, `:41` and `:59`).

### Health check

`GET /api/health` runs `SELECT 1` against the database with a 2 second
timeout and answers 200 `ok`, or 503 `degraded` when the database does not
answer (`backend/internal/server/server.go:45`,
`backend/internal/server/server.go:522-538`). The distroless backend image
has no shell or curl, so the health check is a mode of the binary itself:
`-healthcheck` GETs `/api/health` on localhost and exits 0 or 1
(`backend/cmd/server/main.go:69-81`, `backend/cmd/server/main.go:332-339`,
`deploy/backend.Dockerfile:20-23`). Compose runs it every 30 seconds
(`deploy/compose.yml:20-25`), `frontend` starts only once `backend` is
healthy (`deploy/compose.yml:37-39`), and every service restarts
`unless-stopped` (`deploy/compose.yml:26`, `:40` and `:58`).

### Data on a bind mount

The SQLite file lives in `deploy/data`, bind-mounted at `/data` in the
backend container (`deploy/compose.yml:14`, `deploy/compose.yml:18-19`) and
listed in `.gitignore` (`.gitignore:25`). The reason for a bind mount and
the backup design are in ADR 0002 and are not repeated here.

### Two deployables

The backend and the frontend are separate deployables: a backend Docker
image and a static frontend build served by a web server
(`CLAUDE.md:22`, `docs/PRINCIPLES.md:45`). The frontend is never embedded
in the Go binary (`deploy/README.md:3-4`). The static build is copied into
the nginx image (`deploy/frontend.Dockerfile:9-11`), and the backend image
holds only the binary (`deploy/backend.Dockerfile:9-11`). The reason is not
recorded.

> Superseded 2026-10-07 (GLG-1120): besides the binary, the backend image
> holds `/THIRD_PARTY_NOTICES.md`, the license notices of the dependencies
> (`make notices`). The frontend image copies the file into the html root,
> and nginx serves it as UTF-8 text at `/THIRD_PARTY_NOTICES.md`.

> Superseded 2026-10-08 (GLG-1130): both images also hold the project's
> `LICENSE` (AGPL-3.0), which must travel with the distributed object code:
> the backend image at `/LICENSE`, the frontend image in the html root,
> served by nginx at `/LICENSE` through the same `location` block as the
> notices. In both Dockerfiles the files are the `COPY LICENSE` and
> `COPY THIRD_PARTY_NOTICES.md` lines of the final stage.

## Recorded simplifications

- Every update rebuilds both images on the host (`deploy/RUNBOOK.md:83-86`).
- Errors are seen only in the container logs (`deploy/RUNBOOK.md:253-277`).
- Copying backups off the host is the operator's job (`docs/HISTORY.md`, Phase 4).

> Superseded 2026-10-06 (GLG-1040): several file:line references above have
> drifted and now point at unrelated lines; the claims still hold. The
> `deploy/README.md` and `deploy/RUNBOOK.md` line ranges were off from the
> start, already wrong at 1263843 where this ADR was added. Current
> locations, by name rather than line:
>
> - "reachable only from other containers": `deploy/README.md`, Run locally,
>   the "Published ports" bullet; "only ever talks HTTPS to Caddy": the same
>   section, the "App" and "Published ports" bullets.
> - `ClientIP` in `backend/internal/ratelimit/ratelimit.go`; `TrustProxy` is
>   set and the `slog` JSON logger is built near the top of `main` in
>   `backend/cmd/server/main.go`, after the `-healthcheck` branch.
> - The request log line is in `withLogging` in
>   `backend/internal/server/server.go`; `withRecover` is in the same file,
>   and the middleware chain is the `return` at the end of `newHandler`.
> - The `Secure` flag is set in the `cookie` method of `handler` in
>   `backend/internal/auth/handlers.go`.
> - The `SELECT 1` health check is `handleHealth` in
>   `backend/internal/server/server.go`; the `-healthcheck` flag is handled
>   at the start of `main` and calls `isHealthy` in
>   `backend/cmd/server/main.go`.
> - Rebuilding on the host is `deploy/RUNBOOK.md` §3 Update; errors in the
>   container logs are §7 Logs.
> - Two deployables is `docs/PRINCIPLES.md` #7 (Two deployables, one
>   command).
> - `deploy/data/` is listed in `.gitignore` under `# Local data`.
>
> Commit 1de6fcb (GLG-473) exists and is the CSP change cited above.

> Superseded 2026-10-07 (GLG-1116): the backend no longer takes the first
> `X-Forwarded-For` entry. nginx appends to the header Caddy set, and other
> proxies keep what the client sent, so the first entry could be
> client-chosen. With `TRUST_PROXY=true` the client IP is the entry
> `TRUSTED_PROXY_HOPS` places from the right (default 1); compose sets it to
> 2 for Caddy then nginx.

> Superseded 2026-10-08 (GLG-1127): the frontend image is now
> `nginxinc/nginx-unprivileged` and runs nginx as uid 101, so nginx listens
> on 8080 and Caddy proxies to `frontend:8080`, not `frontend:80`.

> Superseded 2026-10-08 (GLG-1132): the `caddy` service's image is now
> pinned to `caddy:2.11` (GLG-1124), not `caddy:2`. The `deploy/compose.yml`
> line references above have drifted and now point at other lines; the
> claims still hold. Current locations, by name rather than line: the
> `backend`, `frontend` and `caddy` services under `services:`, and in each
> the key the claim names (`build:`, `image:`, `environment:`, `ports:`,
> `volumes:`, `depends_on:`, `healthcheck:`, `restart:`, `logging:`); the
> shared logging options are the `x-logging` anchor at the top of the file.

> Superseded 2026-10-08 (GLG-1135): more line references above have drifted
> and now point at other lines; the claims still hold. Current locations,
> by name rather than line, beyond those the GLG-1040 note gives:
>
> - `TRUST_PROXY` and `COOKIE_SECURE` default to `false` in `Load` in
>   `backend/internal/config/config.go`. The health check's 2 second bound
>   is `healthTimeout` in `backend/internal/server/server.go`.
> - `deploy/nginx.conf`: the client-IP chain is the comment at the top of
>   the file; `/api/` is proxied to `${BACKEND_UPSTREAM}` in
>   `location /api/`; the security headers are the `add_header ... always`
>   lines in `server`, after the comment that explains why they live there
>   and why `script-src 'self'` needs no inline script.
> - `deploy/frontend.Dockerfile`: the static build is the
>   `COPY --from=build /src/dist` line of the final stage, and
>   `BACKEND_UPSTREAM` is substituted through the
>   `/etc/nginx/templates/default.conf.template` copy and its
>   `ENV BACKEND_UPSTREAM` default.
> - `deploy/backend.Dockerfile`: the binary is the
>   `COPY --from=build /out/server` line of the final stage, and the health
>   check is the `HEALTHCHECK` instruction.
> - "Two deployables" in `CLAUDE.md` is the "Prod:" bullet of Tech stack.
