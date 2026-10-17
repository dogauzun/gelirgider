-- One-time recovery codes for two-factor login. code_hash is the sha256 of
-- the normalised code, as in sessions; used_at is NULL until the code is
-- spent. Turning TOTP off or resetting the password deletes the user's rows.
CREATE TABLE totp_recovery_codes (
    user_id INTEGER NOT NULL REFERENCES users(id),
    code_hash TEXT NOT NULL,
    used_at TEXT NULL,
    PRIMARY KEY (user_id, code_hash)
);
