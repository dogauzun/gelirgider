-- Second login step. A password login for a user with TOTP on stores one row
-- here instead of a session; POST /api/auth/login/totp trades the token for a
-- session. token_hash is the sha256 of the token, as in sessions. attempts
-- counts the code checks made with the token, capped at 5 per token and, over
-- a user's recent rows, at 10.
CREATE TABLE login_challenges (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0
);
