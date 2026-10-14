# Release flow: tag main on demand

Date: 2026-10-14

## Problem

`publish.yml` already pushes `latest` and `sha-<7>` images on every merge to
`main` and the tag name on a `v*` tag (ADR 0020), and compose pins through
`IMAGE_TAG` in `deploy/.env`. There is no tag yet, no version in the
binaries, no release notes and no agreed way to decide a version number.
The RUNBOOK tells an operator to pin a `sha-<7>` "or a release tag
`vX.Y.Z` once there is one".

## Goal

One workflow run cuts a release: picks the version, tags `main`, writes
the GitHub Release with notes, publishes the images at that version. A
running instance can say which version it is.

Success: running the `release` workflow on a `main` with merged `feat`
commits since the last tag creates `vX.(Y+1).0`, a GitHub Release with the
merged PR titles, and `ghcr.io/dogauzun/gelirgider-backend:vX.(Y+1).0`
plus `-frontend`; `GET /api/health` on that image returns the version.

## Decisions

- **`main` is the release candidate.** Every merge passes the five required
  checks and review (ADR 0022), and is already published as `latest`.
  No RC branches, no `-rc.N` prerelease tags, no soak window: there is
  no production instance and no tester to soak for. Add prerelease tags
  when both exist.
- **Tag on demand, not on every merge.** Ticket sweeps merge 15 to 20 PRs
  a day; a release per merge is noise. A manual `workflow_dispatch` run
  cuts a release when the owner wants one. No cron.
- **First release is `v1.0.0`.** When no `v*` tag exists, the workflow
  creates `v1.0.0` whatever `bump` says.
- **Version from the squash titles.** Every squash commit subject is a
  Conventional Commits title (`pr-title.yml` enforces it). With
  `bump: auto`, any `feat:` subject since the last tag bumps the minor
  version, anything else bumps the patch version. The major version bumps
  only by the explicit `bump: major` override: the title regex allows no
  `!` or `BREAKING CHANGE` marker, so nothing in the history can mean a
  breaking change.
- **`latest` stays the edge.** `latest` and `sha-<7>` keep tracking `main`.
  A release publishes `vX.Y.Z` and the floating major tag `vX`
  (`v1.4.2` also moves `v1`). Production pins `IMAGE_TAG=v1` and picks up
  minor and patch releases on `docker compose pull`; `IMAGE_TAG=v1.4.2`
  pins one release. No `vX.Y` tag.
- **GitHub auto-generated notes.** `gh release create --generate-notes`
  lists the merged PR titles since the previous tag with a "Full
  Changelog" compare link. The titles already carry the type, the
  description and the `(GLG-n)` ticket, so a grouping script adds little.
  No `CHANGELOG.md`: `docs/HISTORY.md` stays the phase narrative, the
  GitHub Releases page is the per-version record. No `.github/release.yml`
  categories: they group by PR label, and PRs carry no labels.
- **The version is baked into both builds.** `dev` outside a release
  build. Bug reports and the RUNBOOK's "what runs here" need it.

## Components

### `release.yml` (new)

```yaml
on:
  workflow_dispatch:
    inputs:
      bump:
        type: choice
        options: [auto, patch, minor, major]
        default: auto
permissions:
  contents: write # tag + release
  packages: write # for the publish call
```

Job `release`: `actions/checkout@v7` with `fetch-depth: 0` (tags and
history), then one `run:` step in plain shell:

1. `last=$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || true)`.
2. No `last`: `next=v1.0.0`, skip to step 5.
3. `git log "$last"..HEAD --format=%s`. Empty and `bump=auto`: log
   "nothing to release since $last" and exit 0.
4. `auto`: subjects matching `^feat:` bump minor, else patch. `patch`,
   `minor`, `major`: bump that part, zero the parts after it.
5. `gh release create "$next" --target "$GITHUB_SHA" --title "$next" --generate-notes`.
   One call creates the tag and the release. `GH_TOKEN: ${{ github.token }}`.
6. Write `version=$next` to `$GITHUB_OUTPUT`.

Job `publish`: `needs: release`, `if: needs.release.outputs.version != ''`,
`uses: ./.github/workflows/publish.yml` with `version: ${{ needs.release.outputs.version }}`
and `secrets: inherit`. The explicit call is required: a tag created with
`GITHUB_TOKEN` fires no `push` event, so `publish.yml`'s own `tags: ['v*']`
trigger never sees it. A local reusable workflow passes the "GitHub-owned
actions only" policy.

Concurrency group `release`, `cancel-in-progress: false`.

### `publish.yml` (changed)

- Add `on: workflow_call: inputs: version: {type: string, required: false}`.
  Keep the `push` triggers: a tag pushed by hand still publishes.
- Checkout `ref: ${{ inputs.version || github.ref }}`.
- Tags to push: with `inputs.version`, or on a tag push, `vX.Y.Z` and
  `vX` (`v${version%%.*}`). On a `main` push, `latest` and `sha-<7>` as
  today.
- Pass `--build-arg VERSION=<vX.Y.Z or sha-<7>>` to both Dockerfiles.
  Docker warns about an unused build arg and does not fail, so this lands
  before the Dockerfiles consume it.

### Backend version

- `backend/cmd/server/main.go`: `var version = "dev"` set with
  `-ldflags "-X main.version=..."`, passed into `server.New` (or a
  `server.Version` field) and logged once at startup.
- `GET /api/health` returns `{"status":"ok","version":"v1.2.0"}`; the
  degraded body is unchanged. Swag `@Success` description updated,
  `make swagger` run, `backend/apidocs/` committed (ADR 0013).
- `deploy/backend.Dockerfile`: `ARG VERSION=dev`, the `go build` line gets
  `-ldflags "-X main.version=$VERSION"`.
- `Makefile` `build`: `--build-arg VERSION=$$(git describe --tags --always --dirty)`
  for both images.
- Test: the health handler's test asserts the `version` field.

### Frontend version

- `deploy/frontend.Dockerfile`: `ARG VERSION=dev`, `ENV VITE_APP_VERSION=$VERSION`
  before `npm run build`.
- `frontend/vite.config.ts`: `define: { __APP_VERSION__: JSON.stringify(process.env.VITE_APP_VERSION ?? 'dev') }`
  and a `declare const __APP_VERSION__: string` in the env typings.
- `Sidebar.tsx`: a muted one-line label under the account row,
  `t('nav.version', { version })` with `tr` and `en` keys. Rendered only
  when the value is not `dev`, so `make dev` shows nothing.
- Test: the sidebar test renders the label when the define is set.

### Docs

- `deploy/README.md`: the image-tag paragraph names `vX.Y.Z` and the
  floating `vX`, and says `latest` is the edge build from `main`.
- `deploy/RUNBOOK.md`: "pin `IMAGE_TAG=v1`" replaces "once there is one";
  the update step says releases are cut by the `release` workflow in
  Actions and listed on the Releases page.
- `docs/adr/0020-deployment-topology.md`: a dated note recording the
  release flow (tag on demand, version from titles, floating major tag,
  auto notes, no RC).
- `CLAUDE.md` Workflow rules: one line, "Releases: run the `release`
  workflow in Actions; the version comes from the squash titles since the
  last tag, the notes are GitHub's generated notes." `CONTRIBUTING.md`
  gets the same sentence for outside readers.

## Error handling

- Nothing merged since the last tag and `bump: auto`: the run exits 0
  with a log line and creates nothing.
- `gh release create` fails (tag exists, network): the job fails before
  the publish job, no image is pushed for a half-made release.
- Publish fails after the release exists: re-run the failed job from the
  Actions UI; the tag is already there, `publish.yml`'s `workflow_call`
  path is idempotent.
- A tag pushed by hand (`git push origin v1.0.1`) still publishes through
  the `push` trigger, with no GitHub Release; `gh release create` can add
  one later.

## Testing

- `release.yml` logic: the version computation lives in one shell block;
  verify once by running the workflow with `bump: auto` on a `main` with
  no tag and checking `v1.0.0`, its Release and the two `v1.0.0` images on
  the package pages. A second run with no new merges must exit 0 and
  create nothing.
- `publish.yml`: the first release run proves the `workflow_call` path;
  the existing `main` push keeps proving the push path.
- Go: health handler test for the `version` field.
- Web: sidebar test for the version label.
- `make lint` (prettier covers `.github/`), `make swagger` drift check.

## Out of scope

RC branches and prerelease tags, a release cron, a `CHANGELOG.md`,
grouped or Jira-linked notes, `vX.Y` tags, arm64 images, signing.

## Tickets

Epic "Release flow", five children, each a day or less:

| Ticket | Scope                                                                                   | Depends on |
| ------ | --------------------------------------------------------------------------------------- | ---------- |
| a      | `publish.yml`: `workflow_call` input, floating `vX`, `--build-arg VERSION`              | –          |
| b      | `release.yml`: version computation, `gh release create --generate-notes`, publish call  | a          |
| c      | backend version: ldflags, Dockerfile `ARG`, `/api/health` field, swag, `make build` arg | –          |
| d      | frontend version: Dockerfile `ARG`, Vite define, sidebar label, i18n keys               | –          |
| e      | docs: README, RUNBOOK, ADR 0020 note, CLAUDE.md and CONTRIBUTING.md line                | –          |

a, c, d and e are independent; b follows a. The first release is cut by
hand after b, c and d merge.
