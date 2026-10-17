-- A term deposit (vadeli mevduat) of one user in one portfolio. Its
-- instrument (market DEPOSIT) is created with it and goes with it, so the
-- deposit is owned by exactly one row here (UNIQUE instrument_id). The id is
-- in URLs and is never reused (AUTOINCREMENT, as in 027 to 029). principal,
-- rate and stopaj_rate are exact decimal text, the rates as fractions
-- (0.45 for 45%). Dates are YYYY-MM-DD. funding_transaction_id names the
-- cash deposit row that opening the deposit with the fund option booked; it
-- has no foreign key because the user may delete or edit that row on its own.
CREATE TABLE term_deposits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    instrument_id INTEGER NOT NULL UNIQUE REFERENCES instruments(id),
    currency TEXT NOT NULL,
    principal TEXT NOT NULL,
    rate TEXT NOT NULL,
    stopaj_rate TEXT NOT NULL,
    start_date TEXT NOT NULL,
    maturity_date TEXT NOT NULL,
    funding_transaction_id INTEGER,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_term_deposits_user_portfolio ON term_deposits (user_id, portfolio_id);
