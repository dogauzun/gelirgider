# Local overrides and secrets (gitignored, optional). See .env.example.
-include .env
# The backend refuses to start without SECRET_KEY: generate one into .env the
# first time a dev target runs, and keep it (it encrypts the provider keys).
# Skipped when SECRET_KEY_FILE is set: the backend reads the key from that file.
ifneq ($(filter dev dev-backend,$(MAKECMDGOALS)),)
ifeq ($(SECRET_KEY)$(SECRET_KEY_FILE),)
SECRET_KEY := $(shell k=$$(openssl rand -base64 32) && printf '\nSECRET_KEY=%s\n' "$$k" >> .env && chmod 600 .env && echo "$$k")
endif
endif
# Hand them to every target, like compose does for the backend in prod.
export

.PHONY: setup prune-worktrees dev dev-backend dev-frontend test test-backend test-frontend build-frontend build lint swagger swagger-check notices screenshots help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*##"}; {printf "  %-15s %s\n", $$1, $$2}'

# Install the frontend dependencies for this checkout. A worktree cut from the
# same lockfile as the main tree clones the main tree's node_modules instead of
# running npm ci (copy-on-write on APFS and btrfs/xfs, a plain copy elsewhere).
# Never symlink it: npm ci follows the link and deletes the target tree. The
# lockfile checksum in node_modules/.lockfile-sha256 makes a rerun a no-op. A
# main tree without that file (installed by plain npm ci) counts as current
# only when npm wrote its node_modules/.package-lock.json after the lockfile
# last changed; otherwise the worktree runs npm ci itself.
setup: ## Install frontend dependencies, cloning the main checkout's node_modules when the lockfile matches
	@cd frontend && \
	sha() { if command -v sha256sum >/dev/null; then sha256sum "$$1"; else shasum -a 256 "$$1"; fi | cut -d' ' -f1; } && \
	want=$$(sha package-lock.json) && \
	if [ "$$(cat node_modules/.lockfile-sha256 2>/dev/null)" = "$$want" ]; then echo "node_modules up to date"; exit 0; fi && \
	main=$$(git worktree list --porcelain | sed -n '1s/^worktree //p') && \
	if [ "$$(cd .. && pwd -P)" != "$$(cd "$$main" && pwd -P)" ] && [ -d "$$main/frontend/node_modules" ] \
		&& [ "$$(sha "$$main/frontend/package-lock.json")" = "$$want" ] \
		&& { [ "$$(cat "$$main/frontend/node_modules/.lockfile-sha256" 2>/dev/null)" = "$$want" ] \
			|| [ "$$main/frontend/node_modules/.package-lock.json" -nt "$$main/frontend/package-lock.json" ]; }; then \
		echo "cloning node_modules from $$main"; rm -rf node_modules; \
		if [ "$$(uname)" = Darwin ]; then cp -Rc "$$main/frontend/node_modules" node_modules || { rm -rf node_modules; cp -R "$$main/frontend/node_modules" node_modules; }; \
		else cp -R --reflink=auto "$$main/frontend/node_modules" node_modules || { rm -rf node_modules; cp -R "$$main/frontend/node_modules" node_modules; }; fi; \
	else npm ci; fi && \
	echo "$$want" > node_modules/.lockfile-sha256

# Remove the worktrees whose work is finished: not locked, nothing uncommitted,
# and the branch is either an ancestor of origin/main or pushed with -u and gone
# from origin now (GitHub deletes the branch when its PR merges or closes), with
# every local commit still in the remote-tracking ref (fetch runs without
# --prune so that ref survives; commits made after the last push keep the
# branch). A worktree made with `git worktree add -b <b> <path> origin/main`
# starts with upstream origin/main, so the branch must be pushed with -u (as
# CLAUDE.md says) for the gone check to apply; otherwise it stays, the safe
# side. A never-pushed branch with commits and a detached HEAD stay, and so do
# the worktree it runs in, a branch exactly at origin/main and a branch whose
# reflog holds only its creation (both mean just cut: such a branch turns into
# an ancestor of origin/main as soon as any other PR merges). A worktree that
# still holds ignored data files (*.db, .env, *.pdf, deploy/data/) stays too,
# since git worktree remove deletes them. DRY=1 changes
# nothing: it skips the removals and the remote prune. It never runs
# git worktree prune: a worktree whose directory is missing is the user's to
# repair or prune.
prune-worktrees: ## Remove finished worktrees and their local branches (DRY=1 only prints)
	@git fetch origin && heads=$$(git ls-remote --heads origin) && \
	{ printf '%s\n' "$$heads" | grep -q 'refs/heads/main$$' || { echo "git ls-remote origin failed or lists no main: stopping"; exit 1; }; } && \
	heads=$$(printf '%s\n' "$$heads" | cut -f2) && cur=$$(cd "$$(git rev-parse --show-toplevel)" && pwd -P) && \
	git worktree list --porcelain | awk -v RS= -F'\n' 'NR > 1 { p = ""; b = "-"; l = 0; \
		for (i = 1; i <= NF; i++) { if ($$i ~ /^worktree /) p = substr($$i, 10); \
			else if ($$i ~ /^branch refs\/heads\//) b = substr($$i, 19); else if ($$i ~ /^locked/) l = 1 } \
		print p "\t" b "\t" l }' | \
	while IFS="$$(printf '\t')" read -r p b l; do \
		if [ ! -d "$$p" ]; then echo "kept    $$p ($$b): directory missing (repair it with git worktree repair, or git worktree prune; never done here)"; continue; fi; \
		if [ "$$(cd "$$p" && pwd -P)" = "$$cur" ]; then echo "kept    $$p ($$b): current worktree"; continue; fi; \
		if [ "$$l" = 1 ]; then echo "kept    $$p: locked"; continue; fi; \
		if [ "$$b" = - ]; then echo "kept    $$p: detached HEAD"; continue; fi; \
		if [ -n "$$(git -C "$$p" status --porcelain 2>&1)" ]; then echo "kept    $$p ($$b): uncommitted changes"; continue; fi; \
		if [ -n "$$(git -C "$$p" ls-files --others --ignored --exclude-standard -- '*.db' '*.db-*' '.env' '.env.*' '*.pdf' 'deploy/data/')" ]; then echo "kept    $$p ($$b): ignored data files present"; continue; fi; \
		tip=$$(git rev-parse "refs/heads/$$b") && why=""; \
		if [ "$$tip" = "$$(git rev-parse origin/main)" ]; then echo "kept    $$p ($$b): at origin/main, may be just cut"; continue; \
		elif [ "$$(git reflog show --format=%gs "refs/heads/$$b" | wc -l | tr -d ' ')" = 1 ]; then echo "kept    $$p ($$b): no commits yet, may be just cut"; continue; \
		elif git merge-base --is-ancestor "$$tip" origin/main; then why="merged into origin/main"; \
		else up=$$(git for-each-ref --format='%(upstream:short)' "refs/heads/$$b"); \
			case "$$up" in \
				"") echo "kept    $$p ($$b): never pushed, commits not on origin/main"; continue;; \
				origin/*) ;; \
				*) echo "kept    $$p ($$b): upstream $$up is not on origin"; continue;; \
			esac; \
			if printf '%s\n' "$$heads" | grep -Fxq "refs/heads/$${up#origin/}"; then echo "kept    $$p ($$b): upstream still on origin, commits not on origin/main"; continue; fi; \
			if ! git merge-base --is-ancestor "$$tip" "refs/remotes/$$up" 2>/dev/null; then echo "kept    $$p ($$b): upstream gone but unpushed commits (or its remote ref is pruned)"; continue; fi; \
			why="upstream $$up is gone, every commit was pushed"; fi; \
		if [ -n "$(DRY)" ]; then echo "removable $$p ($$b): $$why"; continue; fi; \
		git worktree remove "$$p" && git branch -D "$$b" >/dev/null && echo "removed $$p ($$b): $$why"; \
	done && { [ -n "$(DRY)" ] || git remote prune origin; }

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

# The README screenshots: a throwaway -demo database served by the
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
