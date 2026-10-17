# ADR 0020: Deployment topology

Status: accepted (2026-10-01).

## Context

[ADR 0002](0002-production-datastore-and-migrations.md) records the datastore
and schema files. This ADR records the rest of the deployment decisions. The
how is in `deploy/README.md` (running) and `deploy/RUNBOOK.md` (operating).

## Decision

### Target: Docker Compose on any Docker host

Docker Compose on any Docker host reachable on ports 80 and 443. No VM
provisioning, no cloud-provider steps and no Kubernetes. Backend and frontend
are separate deployables: a Docker image with the Go binary, and an nginx image
with the static build. The frontend is never embedded in the Go binary.

### Request path: Caddy, then nginx, then the backend

`deploy/compose.yml` runs three services on one network.

- `caddy` terminates TLS for `DOMAIN` (Let's Encrypt, or self-signed for
  `localhost`), sets HSTS and proxies to `frontend`. It is the only service
  with published ports, 80 and 443.
- `frontend` is unprivileged nginx serving the static build and proxying
  `/api/` to `backend`.
- `backend` is the Go binary, reachable only from the compose network.

### The backend behind two proxies

Compose sets `TRUST_PROXY=true` and `COOKIE_SECURE=true`; both default to
`false`.

- With `TRUST_PROXY=true` the client IP, used by the auth rate limiter and the
  request log, is the `X-Forwarded-For` entry `TRUSTED_PROXY_HOPS` places from
  the right. Compose sets 2 (Caddy, then nginx, each appending), and Caddy
  discards what the client sent, so the entry cannot be client-chosen.
- `COOKIE_SECURE` marks the session cookie `Secure`, since the browser only
  talks HTTPS to Caddy.

### Content Security Policy

The frontend nginx sets the Content Security Policy and the other security
headers, so they also cover the frontend image run on its own; only HSTS stays
in Caddy. The policy is `script-src 'self'`, so `frontend/index.html` has no
inline script: the theme is applied before first paint by the file
`frontend/public/theme.js`, loaded with a blocking `<script src>`.

### Images come from GHCR

Both images are built on a GitHub Actions runner for `linux/amd64` and
`linux/arm64` (one `docker buildx build`, no emulation: the Go binary
cross-compiles and the frontend bundle is architecture-independent) and
published to `ghcr.io/dogauzun/gelirgider-backend` and `-frontend`. Compose
pulls them with the tag `${IMAGE_TAG:-latest}`; `build:` stays for a modified
checkout. `IMAGE_TAG` pins a `sha-<7>` or `vX.Y.Z` tag for a rollback.

### Release flow

`main` is the release candidate, with no RC branches. A manual run of the
`release` workflow tags it. The version comes from the squash titles since the
last `v*` tag: any `feat` bumps the minor version, anything else the patch
version, the major version only through `bump: major`. The first release is
`v1.0.0`. A release also moves the floating major tag `vX`, so `IMAGE_TAG=v1`
follows minor and patch releases. Notes are GitHub's generated notes; there is
no `CHANGELOG.md`.

### Data, logs and health

- The SQLite file lives in `deploy/data`, bind-mounted into the backend
  container. Backups are `VACUUM INTO` copies through the backend's `-backup`
  flag ([ADR 0002](0002-production-datastore-and-migrations.md)). Copying them
  off the host is the operator's job.
- No external error reporting service. The backend logs JSON lines to stdout
  through `slog`, logs a panic with its stack trace and answers with the
  standard JSON error. Errors are read in the container logs.
- `GET /api/health` runs `SELECT 1` and answers 200, or 503 when the database
  does not answer. The distroless image has no shell or curl, so the compose
  health check is a mode of the binary, `-healthcheck`.
