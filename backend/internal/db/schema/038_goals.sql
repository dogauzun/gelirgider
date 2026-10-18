-- The savings goal of one user: amount in unit (a currency or gold, validated
-- in Go) to reach by target_date, one goal per scope. amount is exact decimal
-- text, target_date a DATE (YYYY-MM-DD, Europe/Istanbul). portfolio_id NULL
-- is the all-portfolios scope; SQLite treats NULLs as distinct in a unique
-- index, so that scope has its own partial index. The id is in URLs and is
-- never reused (AUTOINCREMENT, as in 032).
CREATE TABLE goals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER REFERENCES portfolios(id),
    amount TEXT NOT NULL,
    unit TEXT NOT NULL,
    target_date DATE NOT NULL,
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_goals_portfolio ON goals (user_id, portfolio_id);
CREATE UNIQUE INDEX idx_goals_all ON goals (user_id) WHERE portfolio_id IS NULL;
