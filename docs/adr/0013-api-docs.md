# ADR 0013: API docs (Swagger)

Status: accepted (2026-09-28).

## Context

The backend has over forty JSON routes under `/api/*`. PRINCIPLES #8 asks for
"stable, documented shapes", and without a written description their shapes
live only in Go handlers and in `frontend/src/api.ts`.

## Decision

### swag, run with `go run`, output Swagger 2.0

The spec is generated from [swag](https://github.com/swaggo/swag) annotations
in the handlers' doc comments. `make swagger` runs a pinned swag version with
`go run pkg@version`, so swag stays out of `go.mod`: no module or `tool`
dependency, no runtime import of swaggo, no generated `docs.go`. swag v1 writes
Swagger 2.0 only; OpenAPI 3 would need swag v2, a different tool, and 2.0 is
enough for a dev reference.

The existing annotations in `backend/internal/*/handlers.go` and the swag
variable in the `Makefile` are the reference for how a route is annotated.

### The spec is committed

`backend/apidocs/swagger.json` and `swagger.yaml` are tracked and marked
`linguist-generated` in `.gitattributes`. Only an explicit `make swagger`
writes them, and a route change commits the result with its annotation.
`make lint` runs `make swagger-check`, which runs the same swag command into a
temp dir and fails when the result differs from the committed spec, so a stale
spec or a broken annotation fails lint and lint never writes the tree. On a
merge or rebase conflict in `backend/apidocs/`, take either side, run
`make swagger` and commit the result.

### Dev only, UI from the CDN

The backend serves the docs only when `API_DOCS_DIR` is set. `make dev` and
`make dev-backend` set it to `apidocs`; prod and compose never do.

- `GET /api/docs/` is an inline HTML page that loads `swagger-ui-dist` from
  `cdn.jsdelivr.net` and points it at `/api/docs/swagger.json`. No UI asset
  ships in the binary (PRINCIPLES #7).
- Every other path under `/api/docs/` is a file server on `API_DOCS_DIR`.
- The docs need no session: `main` mounts them on an outer mux in front of
  the API handler. Without `API_DOCS_DIR`, `/api/docs/` falls into the
  protected mux and answers 401.
- The page pins an exact `swagger-ui-dist` version and loads the script and
  stylesheet with `integrity` and `crossorigin="anonymous"`; a bump updates
  the URLs and both sha384 hashes together. The UI runs on the app's origin,
  so a compromised package or CDN could call the API as any admin who opens
  the page. With `COOKIE_SECURE=true`, a production setup, `main` ignores
  `API_DOCS_DIR`, logs one line at startup and does not mount the docs.

### General info

`cmd/server/main.go` carries the title, version, `@BasePath /api` and a
description of the rules that apply to every route: the `session` cookie, 403
`admin only` under `/api/admin/`, the `{"error": "<message>"}` envelope, the
body size cap, 429 with `Retry-After`, and 403 `cross-origin request` for a
write from another origin. Swagger 2.0 has no cookie security scheme, so the
spec declares no `securityDefinitions` and no route carries `@Security`.

A route's error description lists the exact error strings it writes for that
status, because `translateError` in `frontend/src/api.ts` keys off them.

Two swag pitfalls `swagger-check` cannot catch: a repeated `@Failure` code
keeps only the last line, and a type declared inside a method resolves to the
receiver type, an empty object.

### Drift

`TestEveryRouteDocumented` in `backend/internal/server/apidocs_test.go`
compares the `METHOD /api/path` of every registered route with every
`@Router` line. A route without an annotation, or an annotation without a
route, fails `go test ./...` and is named in the output. It checks path and
method only, never schemas, parameters or error strings. Broken type
references are caught by `make swagger-check` in `make lint`.
