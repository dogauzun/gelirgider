# Local overrides and secrets (gitignored, optional). See .env.example.
-include .env
# The backend refuses to start without SECRET_KEY: generate one into .env the
# first time a dev target runs, and keep it (it encrypts the provider keys).
ifneq ($(filter dev dev-backend,$(MAKECMDGOALS)),)
ifeq ($(SECRET_KEY),)
SECRET_KEY := $(shell k=$$(openssl rand -base64 32) && printf '\nSECRET_KEY=%s\n' "$$k" >> .env && chmod 600 .env && echo "$$k")
endif
endif
# Hand them to every target, like compose does for the backend in prod.
export

.PHONY: dev dev-backend dev-frontend test test-backend test-frontend build-frontend build lint swagger swagger-check notices screenshots help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*##"}; {printf "  %-15s %s\n", $$1, $$2}'

dev: ## Run backend and frontend together (Ctrl+C stops both)
	@trap 'kill 0' INT TERM EXIT; \
	(cd backend && HOST=127.0.0.1 API_DOCS_DIR=apidocs go run ./cmd/server) & \
	(cd frontend && npm run dev) & \
	wait

dev-backend: ## Run backend dev server only
	cd backend && HOST=127.0.0.1 API_DOCS_DIR=apidocs go run ./cmd/server

test-backend: ## Run backend go vet and tests
	cd backend && go vet ./... && go test ./...

dev-frontend: ## Run frontend dev server only
	cd frontend && npm run dev

test-frontend: ## Run frontend lint and tests
	cd frontend && npm run lint && npm test

build-frontend: ## Build frontend for production
	cd frontend && npm run build

test: test-backend test-frontend ## Run backend and frontend tests

# swag runs via go run pkg@version, so it stays out of go.mod (ADR 0013).
# -d lists each internal/* package: internal itself has no Go files, and
# swag warns on a search dir that is not a package. Run from backend/; each
# target adds its own -o.
swag = go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g main.go -d ./cmd/server,$$(ls -d internal/*/ | sed 's:^:./:; s:/$$::' | paste -sd, -) --parseFuncBody --ot json,yaml

# golangci-lint runs the same way; .golangci.yml in backend/ picks the linters.
golangci = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

# deadcode too. Run without -test, so a function only tests call counts as
# dead; it exits 0 with findings, so lint fails when it prints anything.
deadcode = go run golang.org/x/tools/cmd/deadcode@v0.48.0

swagger: ## Generate the API spec into backend/apidocs (commit it)
	cd backend && $(swag) -o ./apidocs

# Lint never writes the tree: generate into a temp dir and compare.
swagger-check: ## Fail if backend/apidocs differs from a fresh swag run
	@d=$$(mktemp -d) && trap 'rm -rf "$$d"' EXIT && cd backend && $(swag) -o "$$d" && \
	{ diff -r "$$d" apidocs || { echo "spec stale: run make swagger and commit backend/apidocs"; exit 1; }; }

# The repo-wide prettier check reads the root .gitignore so it skips gitignored
# local files such as .claude/settings.local.json and .claude/worktrees/.
lint: swagger-check ## Run the swag spec check, gofmt, go vet, golangci-lint, deadcode, frontend lint, jscpd, knip, prettier
	@f=$$(gofmt -l backend) || exit 1; if [ -n "$$f" ]; then echo "gofmt needed:"; echo "$$f"; exit 1; fi
	cd backend && go vet ./...
	cd backend && $(golangci) run
	@f=$$(cd backend && $(deadcode) ./...) || exit 1; if [ -n "$$f" ]; then echo "dead code:"; echo "$$f"; exit 1; fi
	cd frontend && npm run lint
	cd frontend && npm run lint:dupl
	cd frontend && npm run knip
	cd frontend && npx prettier --check .
	cd frontend && npx prettier --config .prettierrc.json --ignore-path ../.gitignore --check ../docs ../deploy ../.claude ../.github ../*.md

# go-licenses and license-checker run pinned like swag, so they stay out of
# go.mod and package.json; their first run needs network access. Needs
# frontend/node_modules (npm ci). go-licenses skips the standard library only
# when GOROOT matches the go that compiles, which a toolchain switch from an
# older go breaks: pin both to the toolchain go.mod selects.
golicenses = go run github.com/google/go-licenses@v1.6.0

notices: ## Regenerate THIRD_PARTY_NOTICES.md from the backend and frontend dependencies
	@d=$$(mktemp -d) && trap 'rm -rf "$$d"' EXIT && \
	r=$$(cd backend && go env GOROOT) && m=$$(cd backend && go env GOMODCACHE) && \
	(cd backend && GOROOT=$$r PATH=$$r/bin:$$PATH $(golicenses) report \
		--template ../deploy/notices.tmpl --ignore github.com/dogauzun/gelirgider ./... >"$$d/go.tsv") && \
	(cd frontend && npx --yes license-checker@25.0.1 --production --excludePrivatePackages \
		--json --out "$$d/npm.json" >/dev/null) && \
	node deploy/notices.mjs "$$r/LICENSE" "$$m" "$$d/go.tsv" "$$d/npm.json" >"$$d/notices.md" && \
	mv "$$d/notices.md" THIRD_PARTY_NOTICES.md && \
	cd frontend && npx prettier --config .prettierrc.json --log-level warn --write ../THIRD_PARTY_NOTICES.md

# The README screenshots (GLG-1381): a throwaway -demo database served by the
# backend and `vite preview` of the built frontend on spare ports, captured
# with headless Chrome. The scheduler starts its catch-up before any API call
# could switch it off, so the catch-up, the backfill and every source (the
# names in backend/internal/scheduler/scheduler.go) are switched off in the
# settings table first: the pictures show the seed's prices, and the run needs
# no network. env -i keeps .env out of the backend.
screenshots: build-frontend ## Regenerate docs/screenshots/*.png from the demo seed (needs Google Chrome)
	@d=$$(mktemp -d) && trap 'kill $$b $$v 2>/dev/null; wait; rm -rf "$$d"' EXIT && \
	port() { node -e "const s = require('net').createServer().listen(0, () => { console.log(s.address().port); s.close() })"; } && \
	bp=$$(port) && fp=$$(port) && \
	(cd backend && go build -o "$$d/server" ./cmd/server) && \
	env -i DB_PATH="$$d/demo.db" "$$d/server" -demo >/dev/null && \
	for k in schedule.catchup_enabled schedule.backfill_enabled \
		$$(printf 'source.%s.enabled ' bist bist-splits tcmb us crypto tefas tufe xu100 gold); do \
		echo "INSERT INTO settings (key, value, updated_at) VALUES ('$$k', 'false', datetime('now'));"; \
	done | sqlite3 "$$d/demo.db" && \
	{ env -i HOST=127.0.0.1 PORT=$$bp DB_PATH="$$d/demo.db" SECRET_KEY=$$(openssl rand -base64 32) \
		"$$d/server" >"$$d/server.log" 2>&1 & b=$$!; } && \
	{ (cd frontend && PORT=$$bp exec node_modules/.bin/vite preview --host 127.0.0.1 --port $$fp --strictPort) \
		>"$$d/vite.log" 2>&1 & v=$$!; } && \
	node docs/screenshots/capture.mjs http://127.0.0.1:$$fp || { tail -n 20 "$$d/server.log" "$$d/vite.log"; exit 1; }

build: ## Build backend and frontend Docker images
	docker build --build-arg VERSION=$$(git describe --tags --always --dirty) -f deploy/backend.Dockerfile -t gelirgider-backend .
	docker build --build-arg VERSION=$$(git describe --tags --always --dirty) -f deploy/frontend.Dockerfile -t gelirgider-frontend .
