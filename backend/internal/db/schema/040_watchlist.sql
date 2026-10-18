-- An instrument one user follows without holding it. One row per user and
-- instrument. target_price is exact decimal text in the instrument's
-- currency, NULL when none is set. target_side is 'above' or 'below', fixed
-- from the latest close when the target was set, NULL with no target. The id
-- is in URLs and is never reused (AUTOINCREMENT, as in 039).
CREATE TABLE watchlist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    target_price TEXT,
    target_side TEXT,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_watchlist_user_instrument ON watchlist (user_id, instrument_id);
