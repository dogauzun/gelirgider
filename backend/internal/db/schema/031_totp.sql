-- Two-factor login. totp_secret and totp_pending_secret hold the base32
-- secret sealed by settings.Repo.Seal (AAD "totp:<user id>"): NULL means no
-- TOTP, the pending one lives only during enrolment. totp_last_step is the
-- last accepted 30-second step, so a code cannot be used twice.
ALTER TABLE users ADD COLUMN totp_secret TEXT NULL;
ALTER TABLE users ADD COLUMN totp_pending_secret TEXT NULL;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;
