# gelirgider frontend

React + TypeScript + Vite, TanStack Query, react-router, Tailwind, Base UI, Recharts.
Setup, prerequisites and `make dev` (backend and frontend together) are in the
[root README](../README.md).

Run from `frontend/`:

```
npm ci                  # install dependencies (again after package-lock.json changes)
npm run dev             # dev server on http://localhost:5173, proxies /api to the backend
npm test                # vitest
npm run lint            # eslint
npx prettier --check .  # formatting check; npm run format rewrites
npm run build           # type-check and production build into dist/
```

The dev server proxies `/api` to `http://localhost:$PORT` (default `8080`), the backend's own
variable, so `PORT=8081 make dev` moves both ends together (`vite.config.ts`).

Colors and shadows come from the semantic tokens in `src/index.css`, light and dark; no raw palette
or shadow classes in components ([ADR 0023](../docs/adr/0023-ui-design-system-v2.md)). UI strings go through i18n keys in
`src/i18n/strings.ts`, with a `tr` and an `en` entry.
