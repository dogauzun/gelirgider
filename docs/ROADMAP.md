# Roadmap

This file lists the work in progress, the work planned next and what is out
of scope. Phases map to Jira epics in project `GLG`, and this file and the
epics change together. The decisions behind shipped work are in the ADRs
under [`adr/`](adr/), and the shipped phases are the Done epics of the Jira
project.

## In progress

Nothing at the moment.

## Next

Nothing planned at the moment.

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
