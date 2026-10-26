# Owner decisions

Dated decisions agents do not re-ask. A decision an ADR or `CLAUDE.md` already states is cited, not copied. Changing one is an edit to this file in the PR that changes it.

## Repository and CI

- 2026-10-14: no GitHub organization and no merge queue. A merge queue is unavailable on user-owned repositories, and an organization was chosen and then withdrawn as overkill. Never propose either again. Strict branch protection ("require branches to be up to date") is off and auto-merge is on instead. Accepted cost: two PRs that are green alone but broken together land unchecked, and the push run on `main` catches it.
- 2026-10-09: CI on the owner's Tinker Board was tried and reverted the same day, because it overheats under the build. Never suggest a self-hosted runner on that hardware.
- 2026-10-14 and 2026-10-19: these GitHub settings live outside the repository and are not to be re-proposed:
  - Branch protection on `main` with the seven required contexts, enforce admins, linear history, conversation resolution and 0 required approvals.
  - Actions policy: GitHub-owned actions plus the two review-workflow patterns, SHA pinning required.
  - Fork PR workflow approval for all external contributors.
  - Immutable releases, and a ruleset that blocks deletion, update and force-push of `refs/tags/v*`.
  - CodeQL default setup (go, javascript-typescript), Dependabot alerts and security updates, secret scanning with push protection, private vulnerability reporting.
- Skipped on purpose: a govulncheck / npm audit schedule, a link check, goreleaser, OSSF Scorecard (a third-party action).
- 2026-10-14: the project moved to the public repository `dogauzun/gelirgider` with one squashed root commit; the former repository is archived as `dogauzun/gelirgider-private` (PRs #1 to #1324, Jira keys `GLG-n`). A PR number or `GLG-n` key dated before 2026-10-14 in an ADR or an old squash subject refers to the archived repository, and an old worktree branch cannot be rebased onto the new `main`.
- 2026-10-14: no production instance exists. Nothing is deployed and no scheduler has ever run in production. Every change works from a fresh empty dev DB through `make dev`. The deployment target is Docker Compose on any host ([ADR 0020](../../docs/adr/0020-deployment-topology.md)). Never file a "run X in prod" task.
- Accepted and not to re-flag: the owner's public email on commits and on the GitHub profile.

## Process

- 2026-10-15: measure (review rounds, QA run summaries) before proposing further process cuts. Low code-quality findings stay unticketed: only High and Medium become tasks. No per-finding AST-selector lint rules and no structure-only moves. The `code-quality-check` skill grades findings and files nothing without an approval naming the tasks.
- 2026-10-14: ADRs are decision-only (`docs/adr/README.md`), code comments carry no task ids (`docs/PRINCIPLES.md` #8) and each workflow rule has one home ([CLAUDE.md](../../CLAUDE.md) rule 14).
- 2026-10-12: at most about five implementer agents at once on the owner's machine.
- Chat: replies in English, no widgets, and a decision for the owner goes through the question tool after a short ranked explanation of what each option costs.

## Product and data sources

- License: AGPL-3.0-only. Registration is closed by default ([ADR 0010](../../docs/adr/0010-setup-and-admin.md)). No tax features and no public share links (`docs/ROADMAP.md` non-goals).
- 2026-09-14: the TEFAS `robots.txt` block was accepted knowingly and the source ships opt-in ([ADR 0004](../../docs/adr/0004-tefas-data-source.md)); do not re-raise it for TEFAS. Any other new market-data host gets a `robots.txt` check first and a question to the owner (`CLAUDE.md` coding rules).
- 2026-09-23: İş Yatırım's HTML table is the BIST bonus-issue source ([ADR 0008](../../docs/adr/0008-splits.md)). Provider dividends are suggested, never written automatically, with stopaj 15% for BIST and 20% for US ([ADR 0009](../../docs/adr/0009-dividends.md)).
- Negative positions and cash overdraft are deliberate ([ADR 0019](../../docs/adr/0019-core-engine-and-auth.md)).
- EVDS is used only with a user-supplied key ([ADR 0012](../../docs/adr/0012-benchmarks.md)). Gold is priced through PAXG ([ADR 0017](../../docs/adr/0017-gold.md)).
- 2026-09-29: CSV import keeps row flags across imports and takes no currency column; ambiguous numbers are read like the transaction form ([ADR 0006](../../docs/adr/0006-broker-statement-import.md)).
- 2026-10-01: Garanti dividends are booked as printed with tax 0, and a Garanti brokerage statement never gets an approximated sell: an unknown description is an error row ([ADR 0018](../../docs/adr/0018-garanti-statements.md)).
- 2026-10-07: position details open in a dialog from the row ([ADR 0016](../../docs/adr/0016-position-detail.md)). Deleting `PositionPage`, or adding `all`-portfolio history or transaction routes for it, was rejected.
- 2026-10-20: the Midas zero-commission row is skipped and never booked as a deposit ([ADR 0006](../../docs/adr/0006-broker-statement-import.md)).
