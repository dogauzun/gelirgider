# ADR 0013: API docs (Swagger)

Status: accepted (GLG-510, 2026-09-28); amended 2026-10-06 (GLG-1036, the
spec is committed, only `make swagger` writes it and `make lint` fails on a
stale one); amended 2026-10-08 (GLG-1123, swagger-ui pinned with SRI, docs
off with `COOKIE_SECURE=true`)

## Context

The backend has over forty JSON routes under `/api/*` and no written
description of them. Their shapes live only in Go handlers and in
`frontend/src/api.ts`. PRINCIPLES #8 asks for "stable, documented shapes".
This ADR records the decisions of epic GLG-509 and the annotation
conventions that GLG-511, GLG-512 and GLG-513 follow.

## Decision

### swag, run with `go run`, output Swagger 2.0

The spec is generated from swaggo annotations (comments) by
[swag](https://github.com/swaggo/swag) v1.16.6:

```
make swagger
# cd backend && go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g main.go \
#   -d ./cmd/server,./internal/auth,./internal/benchmark,... --parseFuncBody \
#   -o ./apidocs --ot json,yaml
```

- `go run pkg@version` keeps swag out of `go.mod`: no module or `tool`
  dependency, and no runtime import of swaggo. No `docs.go` is generated
  (`--ot json,yaml`), and `http-swagger` is not used.
- swag v1 writes Swagger 2.0 only. OpenAPI 3 would need swag v2, a
  different tool. 2.0 is enough for a dev reference.
- `--parseInternal` is not needed: types in `internal/` resolve without it.
- `-d` lists every `internal/*` package (the Makefile expands the glob), not
  `./internal`: that dir has no Go files, so swag printed
  `warning: failed to get package name in dir: ./internal`. Sub-packages
  nested deeper are still walked by their parent's search dir.
- The swag v1.16.6 binary reports its version as `v1.16.4`. That is an
  upstream quirk; the module version is what `go run` pins.

`make lint`, `make dev` and `make dev-backend` run `make swagger` first, so
a broken annotation fails there. swag exits non-zero on a type it cannot
find (`cannot find type definition`).

> Superseded 2026-10-06 (GLG-1036): `make dev` and `make dev-backend` no
> longer generate the spec; they serve the committed one. Only an explicit
> `make swagger` writes `backend/apidocs/`. `make lint` runs
> `make swagger-check`, which runs the same swag command into a temp dir and
> fails with `spec stale: run make swagger and commit backend/apidocs` when
> the result differs from the committed spec, so a broken annotation still
> fails lint and lint never writes the tree. The Makefile keeps the swag
> flags in one `swag` variable; only `-o` differs between the two targets.

### The spec is generated, never committed

`backend/apidocs/` is gitignored. Three annotation tickets run in parallel;
a committed spec would conflict in every one of them, and it would go stale
whenever someone changes a route without regenerating it. Generating it on
every `make lint` and dev start costs about a second.

> Superseded 2026-10-06 (GLG-1036): the spec is committed.
> `backend/apidocs/swagger.json` and `swagger.yaml` are tracked and marked
> `linguist-generated` in `.gitattributes`. `make swagger` regenerates them;
> commit the result with the annotation change. `make lint` catches a stale
> spec. On a merge or rebase conflict in `backend/apidocs/`, do not
> hand-merge: take either side, run `make swagger` and commit the result.

### Dev only, UI from the CDN

The backend serves the docs only when `API_DOCS_DIR` is set.
`make dev` and `make dev-backend` set it to `apidocs` (relative to
`backend/`). Prod and compose never set it.

- `GET /api/docs/` is an inline HTML page that loads `swagger-ui-dist@5`
  from `cdn.jsdelivr.net` and points it at `/api/docs/swagger.json`. No UI
  asset ships in the binary (PRINCIPLES #7).
- Every other path under `/api/docs/` is a file server on `API_DOCS_DIR`
  (`swagger.json`, `swagger.yaml`).
- `main` mounts it on an outer mux in front of `server.New`'s handler, so
  the docs need no session and `server.New` is unchanged. Without
  `API_DOCS_DIR`, `/api/docs/` falls into the protected mux and answers 401.

> Amended 2026-10-08 (GLG-1123): the page pins an exact
> `swagger-ui-dist` version (comment in `internal/server/docs.go`) and
> loads the script and stylesheet with `integrity` and
> `crossorigin="anonymous"`; a bump updates the URLs and both sha384
> hashes together. The UI runs on the app's origin, so a compromised
> package or CDN could call the API as any admin who opens the page. With
> `COOKIE_SECURE=true`, a production setup, `main` ignores `API_DOCS_DIR`,
> logs one line at startup and does not mount the docs. The files stay on
> the CDN, not vendored.

### General info

`cmd/server/main.go` carries `@title gelirgider API`, `@version 1`,
`@BasePath /api` and a `@description` with the rules that apply to every
route: the `session` cookie from `POST /api/auth/login`, 403 `admin only`
under `/api/admin/`, the `{"error": "<message>"}` envelope, the 64 KB body
cap (except the two statement import routes), 429 `too many requests`
with `Retry-After`, and 403 `cross-origin request` for a POST, PUT, PATCH
or DELETE from another origin. Swagger 2.0 has no cookie security scheme,
so the spec declares no `securityDefinitions` and no route carries
`@Security`.

`@Router` paths are relative to `@BasePath`: `@Router /health [get]` for
`GET /api/health`.

### Conventions for annotations

All of these were tried against swag v1.16.6 while writing this ADR.

**Where the block goes.**

- On the named handler: a func, a method (`func (h *handler) list`), or a
  factory func that returns the handler
  (`func handleHealth(...) http.HandlerFunc`). swag reads the block from the
  doc comment, not from the registration.
- Inline closure (`mux.HandleFunc("...", func(w, r) {...})`): put the
  block as a comment directly above the `HandleFunc` call.
  `--parseFuncBody` picks it up. Tried on `PUT /api/admin/sources/{name}`
  in `server/schedule.go`. No need to give the closure a name.
- The block goes after the prose doc comment, separated by an empty `//`
  line, tab-aligned as gofmt leaves it:

```go
// handleHealth checks the database is reachable before reporting healthy,
// so a missing or corrupted database file doesn't report ok.
//
//	@Summary	Health check
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	map[string]string	"status is ok"
//	@Failure	503	{object}	map[string]string	"status is degraded, error is database unavailable"
//	@Router		/health [get]
```

**Tags.** One `@Tags` per route, by resource: `auth`, `setup`,
`portfolios`, `transactions`, `instruments`, `prices`, `imports`,
`dividends`, `health`. Everything under `/api/admin/` is `admin`,
whatever its resource.

**Errors.** Use the inline envelope, which swag renders as an object with
one `error` string property:

```go
//	@Failure	400	{object}	object{error=string}	"invalid request body; at: must be HH:MM"
```

No shared `apiError` type. The description lists the exact error strings
the route writes for that status, separated by `; `, because
`translateError` in `frontend/src/api.ts` keys off them. One `@Failure`
line per status code: swag keeps only the last line for a repeated code,
with no warning. Leave out the 401, 403 `admin only` and 429 that the general
description already covers, unless the handler itself writes a different
message for that status.

**Types.**

- Named types, exported or not, are referenced as `pkg.Type`:
  `transaction.transactionResponse`, `server.sourceSettings`. A bare
  `transactionResponse` also resolves inside its own package, but always
  write the package so blocks read the same everywhere. No import is
  needed; swag resolves from source.
- A named type declared inside a plain func resolves as `pkg.Func.Type`
  (tried: `server.probeFunc.probe`). Inside a method it does not: swag
  silently resolves `server.dividendHandler.expected.held` to the receiver
  type `server.dividendHandler`, an empty object. Do not use that form on
  methods.
- An anonymous request struct (`var req struct{...}`) has no name to
  reference. Describe it inline:
  `@Param body body object{enabled=bool,at=string} true "settings"`.
- Do not refactor code for docs: no new named types just to reference them.
- Decimals are already `string` in JSON, so they need no `swaggertype` or
  `.swaggo` override.
- Arrays: `@Success 200 {array} transaction.transactionResponse`.

**Parameters, headers, uploads.**

- Path: `@Param id path int true "portfolio id"`.
- Query: `@Param limit query int false "page size, 1-100"`.
- JSON body: `@Accept json` and `@Param body body pkg.Type true "desc"`.
- Response header: `@Header 200 {string} X-Next-Cursor "desc"`. It must
  come after the `@Success 200` line: swag attaches a header only to a
  response it has already seen.
- Multipart upload: `@Accept multipart/form-data` and
  `@Param file formData file true "desc"`.

### Drift

`TestEveryRouteDocumented` in `backend/internal/server/apidocs_test.go`
(GLG-514) scans the non-test Go source under `backend/internal` and compares
two sets of `METHOD /api/path`:

- registered: the first argument of every `Handle`/`HandleFunc` call,
  either a `"METHOD /api/..."` literal or `"METHOD "+Const` where `Const`
  is a `const` string declared under `backend/internal` (the statement
  import routes use this form);
- documented: every `@Router` line, with `/api` prepended.

A route without an annotation, or an annotation without a route, fails
`go test ./...` and is named in the output. Mounts without a method
(`mux.Handle("/api/", ...)`) are skipped. Any other pattern form (a
variable, a helper) fails the test as unrecognized, so a new form cannot
slip past silently; extend the test when one is needed. `cmd/server` is
not scanned, so the dev-only `GET /api/docs/` needs no exclusion.

It checks path and method only, never schemas, parameters or error
strings, and it never reads the generated spec. Broken type references are
caught by `make swagger`, which `make lint` runs.

## Consequences

- GLG-510 adds `make swagger`, the dev-only `/api/docs/` UI, the general
  info, the `GET /api/health` annotation and this ADR.
- GLG-511 annotates the auth routes.
- GLG-512 annotates portfolio, transaction, instrument, price and statement
  routes.
- GLG-513 annotates the `server` package routes, including the inline
  closures in `schedule.go` and `providers.go`.
- GLG-514 adds the drift test.
- A change to a route's shape or error strings now also means a change to
  its annotation block.
