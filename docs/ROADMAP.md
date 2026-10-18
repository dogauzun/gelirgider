# Roadmap

This file lists the work in progress, the work planned next and what is out
of scope. Phases map to Jira epics in project `GLG`, and this file and the
epics change together. The decisions behind shipped work are in the ADRs
under [`adr/`](adr/), and the shipped phases are the Done epics of the Jira
project.

## In progress

Nothing at the moment.

## Next

- Phase 32, quick wins (GLG-1460): small improvements from the Ghostfolio review, such as a real return next to TWR, upcoming dividends by month, `SECRET_KEY_FILE` and a 15,000-transaction benchmark.
- Phase 36, configurable attention rules (GLG-1471): each user switches the Dikkat rules off and tunes their thresholds, with two new TRY rules.
- Phase 34, term deposits (GLG-1464): record a vadeli mevduat and value it daily with interest accrued net of stopaj.
  - Deferred: renewing a matured deposit.
- Phase 37, two-step login with TOTP (GLG-1472): an optional TOTP second step at login, with recovery codes and a QR code at enrolment.
- Phase 33, return split (GLG-1461): split a range's return into price, income and currency, per portfolio and per position.
  - Deferred: the Katkı card's range mode.
- Phase 35, allocation by asset class (GLG-1463): group the allocation card by equity, fund, commodity, crypto, deposit and cash.
  - Deferred: the sector view, if GLG-1475 finds no BIST sector source.
- Phase 38, target allocation and rebalancing (GLG-1566): target weights per asset class, drift next to the actual share, a drift Dikkat rule and a buy-only planner that splits a contribution across the under-weight classes.
- Phase 39, goals and real-terms projection (GLG-1567): a goal per portfolio in TRY, real TRY or USD, projected from the portfolio's own returns and contributions as a fan to the target date, with the month it is reached or the monthly gap.

A new phase is added here, together with its Jira epic, once it is planned.

## Non-goals

- No native apps; an installable PWA without a service worker exists.
- Real-time streaming prices.
- Tax reporting.
- Social or copy-trading features.
- Kubernetes. Docker Compose is the only deployment target; a Kubernetes epic may come later.
- Email alerts and a public share link, dropped from Phase 6 (GLG-284).
- An external error reporting service
  ([ADR 0020](adr/0020-deployment-topology.md)).
- OpenAPI 3 and API docs in production
  ([ADR 0013](adr/0013-api-docs.md)).
- Self-service password reset. The operator or an admin resets a forgotten
  password ([ADR 0019](adr/0019-core-engine-and-auth.md)).
- User-entered splits. Splits come from providers, with an operator CLI flag
  as the fallback ([ADR 0008](adr/0008-splits.md)).
- Invite links and an audit log of admin actions
  ([ADR 0010](adr/0010-setup-and-admin.md)).
