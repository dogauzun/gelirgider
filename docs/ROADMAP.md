# Roadmap

This file lists the work in progress, the work planned next and what is out
of scope. Phases map to Epic tasks in the gelirgider Tasks database in
Notion, and this file and the epics change together. The decisions behind
shipped work are in the ADRs under [`adr/`](adr/), and the shipped phases are
the Done epics of the Tasks database.

## Product framing

gelirgider is built for Turkish retail investors in TEFAS funds and BIST. Their
yardstick is the real return against USD, not the official inflation index. It
has no tax features. It is open source, and a paid hosted tier is a possibility
later.

## In progress

Nothing at the moment.

## Next

Nothing at the moment.

A new phase is added here, together with its Epic task, once it is planned.

### Candidates

Not planned and not ticketed.

- Concentration warnings ("X-ray lite").
- Asset-class allocation. It needs the TEFAS fund type, which is not stored today.
- In-kind transfer of a position between portfolios.
- Position page extras: dividends received, yield on cost, 52-week high and low.
- Manually valued "other" assets, reusing the manual gold-coin price path.
- A market strip on Home from the benchmark values.
- BES pension funds, through the TEFAS fund type `EMK`.

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
- FIRE calculator: the 4% rule does not hold under Turkish inflation.
- Sector and country allocation: it needs a new data source, with a robots.txt risk.
- Tags.
- OIDC, WebAuthn and MCP login.
- Treemap.
- Fear & Greed index.
