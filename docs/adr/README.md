# Architecture decision records

An ADR records a decision that later work has to follow and that the code
alone does not explain, such as a data source chosen in a spike, the design
of a phase epic, or a workflow rule.

## Index

| ADR  | Title                                                                                                              | Status                                            |
| ---- | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| 0001 | [BIST end-of-day price source](0001-bist-price-source.md)                                                          | accepted 2026-09-13                               |
| 0002 | [Production datastore and schema migrations](0002-production-datastore-and-migrations.md)                          | accepted 2026-09-13                               |
| 0003 | [Multi-currency model, TCMB FX source, US price source](0003-multi-currency-and-market-data-sources.md)            | accepted 2026-09-14                               |
| 0004 | [TEFAS fund list and daily NAV source](0004-tefas-data-source.md)                                                  | accepted 2026-09-14                               |
| 0005 | [UI design system ("Defter") — semantic tokens, fonts, theming, native dialogs](0005-ui-design-system.md)          | superseded by [0023](0023-ui-design-system-v2.md) |
| 0006 | [Broker statement import — PDF text extraction, partial parsers, two-step import](0006-broker-statement-import.md) | accepted 2026-09-21                               |
| 0007 | [Crypto — BtcTurk public API, market `CRYPTO`, TRY and USD pairs](0007-crypto.md)                                  | accepted 2026-09-23                               |
| 0008 | [Stock splits and bonus issues](0008-splits.md)                                                                    | accepted 2026-09-23                               |
| 0009 | [Dividends from provider data](0009-dividends.md)                                                                  | accepted 2026-09-23                               |
| 0010 | [First-time setup and admin](0010-setup-and-admin.md)                                                              | accepted 2026-09-23                               |
| 0011 | [Portfolio returns](0011-portfolio-returns.md)                                                                     | accepted 2026-09-25                               |
| 0012 | [Benchmarks](0012-benchmarks.md)                                                                                   | accepted 2026-09-27                               |
| 0013 | [API docs (Swagger)](0013-api-docs.md)                                                                             | accepted 2026-09-28                               |
| 0014 | [All-portfolios view](0014-all-portfolios-view.md)                                                                 | accepted 2026-09-28                               |
| 0015 | [PR review by a cloud routine, no GitHub Actions](0015-pr-review-routine.md)                                       | superseded by [0022](0022-pr-review-agent.md)     |
| 0016 | [Position detail page](0016-position-detail.md)                                                                    | accepted 2026-09-28                               |
| 0017 | [Gold](0017-gold.md)                                                                                               | accepted 2026-09-28                               |
| 0018 | [Garanti BBVA statements](0018-garanti-statements.md)                                                              | accepted 2026-10-01                               |
| 0019 | [Core engine and auth](0019-core-engine-and-auth.md)                                                               | accepted 2026-10-01                               |
| 0020 | [Deployment topology](0020-deployment-topology.md)                                                                 | accepted 2026-10-01                               |
| 0021 | [i18n and CSV export](0021-product-and-ui-decisions.md)                                                            | accepted 2026-10-01                               |
| 0022 | [PR review by a subagent of the merger](0022-pr-review-agent.md)                                                   | accepted 2026-10-02                               |
| 0023 | [UI design system v2 ("soft depth") — Inter, shadow tokens, Base UI, sidebar shell](0023-ui-design-system-v2.md)   | accepted 2026-10-04                               |
| 0024 | [Term deposits valued by a computed price source](0024-term-deposits.md)                                           | accepted 2026-10-18                               |
| 0025 | [Two-step login with TOTP](0025-two-step-login.md)                                                                 | accepted 2026-10-18                               |
| 0026 | [Instrument classification](0026-instrument-classification.md)                                                     | accepted 2026-10-18                               |
| 0027 | [Return attribution](0027-return-attribution.md)                                                                   | accepted 2026-10-18                               |
| 0028 | [Target allocation and the contribution planner](0028-target-allocation.md)                                        | accepted 2026-10-18                               |
| 0029 | [Goals and the real-terms projection](0029-goals-and-projection.md)                                                | accepted 2026-10-18                               |
| 0030 | [Saving plans](0030-saving-plans.md)                                                                               | accepted 2026-10-18                               |

## Convention

- An ADR states what holds now: Context, Decision, and open questions if any.
  No Consequences ticket lists, no spike or curl logs, no tables that the code
  or `index.css` owns, no `file.go:NNN` citations.
- The status line is line 3: `Status: accepted (YYYY-MM-DD).` or
  `Status: superseded by [ADR NNNN](NNNN-file.md) (YYYY-MM-DD).`
- When a decision changes, rewrite the body so it reads true today; the git
  log of the file is the history. A replaced decision gets one sentence
  pointing at the ADR that replaced it. No dated blockquote notes.
- A ticket key may appear only where its Jira comments hold evidence the ADR
  summarizes (a spike result). Otherwise no ticket ids.
- A new ADR takes the next free number and gets a row in the index above.
