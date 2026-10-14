# Deploy

Backend and frontend are separate images. The frontend is never embedded in
the Go binary.

## Build

```
make build
```

This runs, from the repo root:

```
docker build -f deploy/backend.Dockerfile -t gelirgider-backend .
docker build -f deploy/frontend.Dockerfile -t gelirgider-frontend .
```

This is a standalone image build, not a step of the compose run below, and
compose never uses the `gelirgider-backend` and `gelirgider-frontend` tags.
`deploy/compose.yml` pulls the published images below by default (`image:`,
at the tag `IMAGE_TAG` in `deploy/.env` names, `latest` when unset) and
builds from the same Dockerfiles (`build:`) only with `--build` or when the
pull fails. Use `make build` to check that the images build or to get
tagged images for another runtime.

## Published images

`.github/workflows/publish.yml` publishes both images, `linux/amd64` only, to
the GitHub Container Registry:

- `ghcr.io/dogauzun/gelirgider-backend`
- `ghcr.io/dogauzun/gelirgider-frontend`

A push to `main` publishes `latest` and `sha-<first 7 of the commit sha>`; a
`v*` tag publishes the tag name, for example `v1.2.0`. Compose pulls `latest`
unless `IMAGE_TAG` in `deploy/.env` pins another tag (`deploy/RUNBOOK.md`,
Rollback). An arm64 host without amd64 emulation cannot run them (the
backend exits with `exec format error`) and builds from the checkout with
`up --build` instead.

## Run locally (production-like)

```
cp deploy/.env.example deploy/.env
openssl rand -base64 32
```

Paste the generated key into the existing `SECRET_KEY=` line of
`deploy/.env` (do not append a second `SECRET_KEY` line). Make the env file
and the data directory private: the backend runs as the distroless
`nonroot` user, uid and gid 65532, and must own `deploy/data`, which holds
the database and its backups (password hashes, encrypted provider keys):

```
chmod 600 deploy/.env
sudo mkdir -p deploy/data
sudo chown 65532:65532 deploy/data
sudo chmod 700 deploy/data
```

Then start the stack:

```
docker compose -f deploy/compose.yml up --build
```

`--build` builds both images from this checkout and tags them with the
published names, so they stand in for the pulled ones until the next
`docker compose -f deploy/compose.yml pull`. Without it, compose runs the
images it already has and pulls the published ones when it has none.
Compose refuses to start without `SECRET_KEY`. On first start the backend
logs a `setup_token`; open https://localhost/setup and enter it to create the
admin (`deploy/RUNBOOK.md`, First deploy).

- App: https://localhost — Caddy terminates TLS here. With `DOMAIN=localhost`
  (the `.env.example` default) Caddy issues a local self-signed certificate,
  so the browser will show a certificate warning; accept it to proceed. Set
  `DOMAIN` to a real hostname in production and Caddy gets a Let's Encrypt
  certificate automatically instead.
- Published ports: only `80` (redirects to HTTPS) and `443`, both on the
  `caddy` service. `frontend` and `backend` are reachable only from other
  containers on the compose network.
- Caddy proxies to `frontend:8080`, which proxies `/api` to `backend:8080`.
- The backend's SQLite file lives on a bind mount at `./data` next to this
  compose file (`DB_PATH=/data/gelirgider.db` inside the container). See
  `deploy/RUNBOOK.md` for how backups are actually taken. Without the
  `chown` above, Docker creates `./data` owned by root and the backend
  cannot create its database there.
- The backend container runs as uid 65532 with a read-only root filesystem
  (only `/data` and a `/tmp` tmpfs are writable), every Linux capability
  dropped and `no-new-privileges`. Commands run in it with
  `docker compose exec` or `run` (backup, `-set-password`, `-fetch`) run the
  same way.
- The frontend container runs nginx as uid 101 (`nginx-unprivileged`, so it
  listens on 8080) with a read-only root filesystem (only `/tmp` and
  `/etc/nginx/conf.d` are tmpfs), every capability dropped and
  `no-new-privileges`.

## Config

- `deploy/.env` (see `deploy/.env.example`): `DOMAIN`, the hostname Caddy
  requests a certificate for, and `SECRET_KEY`, base64 of 32 random bytes
  that encrypts the provider API keys stored in the database. Generate it
  once and keep it; see `deploy/RUNBOOK.md`, Prerequisites.
- Backend (`backend/internal/config/config.go`), each with its default when
  unset:
  - `HOST` (default empty, which binds every interface), the address the
    backend listens on. Compose leaves it unset; `make dev` and
    `make dev-backend` set `127.0.0.1`.
  - `PORT` (default `8080`), the port the backend listens on.
  - `DB_PATH` (default `./gelirgider.db`), the SQLite file, relative to the
    directory the backend runs in.
  - `COOKIE_SECURE` (default `false`), marks the session cookie `Secure`.
  - `TRUST_PROXY` (default `false`), reads the client IP from
    `X-Forwarded-For` instead of the connection address.
  - `TRUSTED_PROXY_HOPS` (default `1`, used only with `TRUST_PROXY`), the
    number of proxies in front of the backend that append to
    `X-Forwarded-For`; the client IP is the entry that many places from the
    right. With `TRUST_PROXY` on, the backend refuses to start if it is not an
    integer of at least 1.
  - `SECRET_KEY` (no default; the backend refuses to start without it),
    described above.
  - `API_DOCS_DIR` (default empty, which turns the API docs off), the
    directory with the generated `swagger.json` that the backend serves at
    `/api/docs/`. Only `make dev` and `make dev-backend` set it; compose
    never does, and the backend ignores it when `COOKIE_SECURE` is `true`,
    so production serves no API docs (ADR 0013).

  `COOKIE_SECURE` and `TRUST_PROXY` are enabled only by the literal value
  `true`; any other value leaves them off. Compose sets both to `"true"`,
  because the backend sits behind Caddy and nginx there, and sets
  `TRUSTED_PROXY_HOPS` to `2` for those two proxies.

- Frontend image: `BACKEND_UPSTREAM` (default `backend:8080`), the host:port
  nginx proxies `/api` to.

## Operations

For running this in an actual deployment — first deploy, updating, backups
and the backup cron, a restore drill, reading logs, and rolling back — see
[`deploy/RUNBOOK.md`](./RUNBOOK.md).
