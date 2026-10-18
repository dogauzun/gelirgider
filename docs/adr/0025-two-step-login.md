# ADR 0025: Two-step login with TOTP

Status: accepted (2026-10-18).

## Context

Email and password with a server-side session ([ADR 0019](0019-core-engine-and-auth.md))
is the only way in. A leaked or guessed password gives full access to a
user's portfolios. Users who want more can add a time-based one-time code
(TOTP, an authenticator app) as a second step. The product has no mail, so
nothing can be reset by email, and the operator or an admin is the recovery
path ([ADR 0010](0010-setup-and-admin.md)).

## Decision

### The code

- **RFC 6238 from the standard library**: HMAC-SHA1, 6 digits, 30 second
  steps, a 20 byte random secret shown as base32, and a window of one step on
  each side of now. No TOTP dependency.
- **The secret is sealed with AES-256-GCM** under `SECRET_KEY`, stored as
  base64 of nonce and ciphertext, like the provider keys. The additional
  authenticated data is `totp:<user_id>`, so a sealed secret copied to another
  user's row does not open. A user with no secret has two-step login off.
- **A code works once.** The user row keeps `totp_last_step`, the step of the
  last accepted code. A code is accepted only for a step above it, and the
  update is a compare-and-set in one statement, so two requests with the same
  code cannot both succeed.
- **TOTP stays optional.** No setting makes it mandatory, for any user or for
  admins.
- **The QR code is drawn in the browser by `uqr`**, the one frontend
  dependency this ADR adds (MIT, no dependencies of its own, about 8 kB
  gzipped before tree shaking, maintained under the unjs organisation). Only its
  `encode` is used, and the account page draws the module matrix as inline
  SVG, so the secret never goes to an image service. The otpauth address and
  the key are shown as text next to it.
- **There is no "remember this device".** A session already lasts 30 days, so
  a device cookie would save almost nothing and add a way around the code
  step.

### Setup, recovery codes and turning it off

- **Setup needs the current password.** The server makes a secret and returns
  it with its `otpauth://` URI, keeping it sealed in a separate pending
  column. It becomes the active secret only when the user sends a valid code
  from it, and only then are the recovery codes issued. A new setup replaces
  the pending secret.
- **Ten recovery codes**, random, shown once. Only a hash is stored, a code
  works once: using it sets its `used_at`, and the row stays until new codes
  replace the set or two-step login is turned off or reset. A used code goes
  through the same attempt caps as a TOTP code. Making new codes needs the
  current password and replaces the old ones.
- **Turning it off needs the current password and a valid code** (TOTP or
  recovery), and deletes the secret, the last step and the recovery codes.
- **`SetPassword` clears two-step login.** The operator flag and the admin API
  both end up in `SetPassword`. Besides signing out every session of that
  user, it now deletes the secret, the pending secret, the last step, the recovery
  codes and any open login challenges. This is the reset for a lost authenticator or for an
  unreadable secret (below). Whoever can call it can already take over the
  account, so nothing is lost. A password change by the user does not clear
  it.

### Login

- **A correct password for a user with two-step login on creates no session.**
  It creates a login challenge row and returns its token. A second request
  with the token and a code creates the session. The token is random and only
  its sha256 is stored, like a session token. A challenge lasts 5 minutes and
  is deleted when used.
- **Attempt caps.** A challenge allows 5 wrong codes and is then deleted, so
  the user starts again with the password. A user allows 10 wrong codes in 15
  minutes across all their challenges; past that, a login with the correct
  password for that user also gets 429 until the window passes. The second step
  route is under the per-IP auth rate limit of ADR 0019.
- **The accepted cost of the per-user cap.** Someone who knows a user's
  password can send 10 wrong codes and keep that user out of the code step,
  and so out of their own login (429), for the 15 minute window, and again
  when it ends. The cap is kept because without it the 5 per token limit can
  be restarted forever with the password and a 6 digit code can be guessed.
  A password-only attacker is locked out of the account either way; the owner
  waits or the operator resets the user.
- **It fails closed.** A user whose secret does not open (below) cannot log in:
  the server never falls back to the password alone.

### `SECRET_KEY`

- **Rotating or losing `SECRET_KEY` makes every TOTP secret unreadable** and
  locks every user who turned on two-step login out, until an admin or the
  operator resets that user (`SetPassword`) and the user enrols again. Users,
  portfolios and transactions are not affected, and users without two-step
  login can still log in. Until the reset, a correct password still returns
  the challenge token and the code step answers 401, and confirm, turn-off and
  new recovery codes answer 409, all with "two-step secret unreadable, ask an
  admin to reset your password", so the user learns what to do.
  `deploy/RUNBOOK.md` says so where the key is
  described.
