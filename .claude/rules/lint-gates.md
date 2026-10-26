---
paths: ['backend/**', 'frontend/**']
---

# Lint gates

What an agent has to satisfy beyond what `make lint` prints.

## Backend

- `forbidigo` (`backend/.golangci.yml`) forbids four calls in new Go code, each with its replacement:
  - `time.LoadLocation`: use `price.Istanbul`.
  - `strings.Repeat`: use `db.InList`.
  - `price.MinDate`: use `price.CheckDate`.
  - `price.Istanbul` itself: use the price day helpers (only the scheduler, the manual run, the TEFAS window and tests may use the zone directly).
- No new `nolint:revive`: fix the signature instead. Lowering `argument-limit` below its current value would hit many functions: ask first.
- The thresholds (`gocognit`, `nestif`, `funlen`, `argument-limit`, `file-length-limit`, `dupl`) are listed in `docs/PRINCIPLES.md` #6 and `backend/.golangci.yml`; read them there.
- The first `make lint` compiles golangci-lint and takes minutes.
- CodeQL default setup flags any Go variable whose name contains "password" and any `make` whose size traces to request input. Name and size things accordingly.

## Frontend

- `lib/`, `api/` and `i18n/` never import components (ESLint layer boundary, `frontend/eslint.config.js`).
- Remembered choices go through `useStoredChoice` or `createStore`, never raw `localStorage`.
- Cache busting goes through `invalidatePortfolio` / `invalidatePortfolios` from `api/transactions`.
- `max-params` is 4 (`frontend/eslint.config.js`).
