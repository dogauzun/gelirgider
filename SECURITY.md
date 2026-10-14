# Security policy

gelirgider is a multi-user app that holds people's portfolios, password hashes
and encrypted provider API keys. Please report security problems privately, not
in a public issue or pull request.

## Reporting a vulnerability

Use GitHub private vulnerability reporting: open the repository's **Security**
tab and choose **Report a vulnerability**. Describe the problem, how to
reproduce it and what an attacker could do with it. Only the maintainers see the
report.

## Supported versions

Only `main` is supported. There are no releases or maintained older branches,
so fixes land on `main`.

## What counts

Anything that lets someone read or change what they should not, for example:

- Authentication and sessions: logging in as someone else, getting past the
  setup or admin checks, registering while registration is closed.
- User scoping: reading or changing another user's portfolios, transactions,
  manual prices or settings (every user-data query must filter by `user_id`).
- Statement and CSV import: a crafted file that crashes the server, reads files
  or data it should not, or writes into another user's data.
- Provider API keys: a key leaking into a response, a log or the frontend, or
  being readable without the `SECRET_KEY` that encrypts it.

Bugs that need an attacker who already controls the server, its environment or
the database file are out of scope.

## What to expect

This is a small project maintained in spare time. Expect a first reply within a
week. Once the problem is confirmed, it is fixed on `main` as soon as we can,
and you are credited in the fix if you want to be.

There is no bug bounty.
