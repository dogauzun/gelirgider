# ADR 0015: PR review by a cloud routine, no GitHub Actions

Status: superseded by [ADR 0022](0022-pr-review-agent.md) (2026-10-02).

This ADR decided three things: every PR is reviewed by a cloud routine fired by
a GitHub webhook and an hourly schedule, the merge gate is a `Verdict:`
comment that routine posts on the PR head, and the repository runs no GitHub
Actions, so every check runs locally before merge.

All three were replaced. [ADR 0022](0022-pr-review-agent.md) replaced the
routine and its verdict with a review subagent that the merger spawns. The
required GitHub Actions checks (`docs/PRINCIPLES.md` #6) replaced the
no-Actions rule and the local check run.
