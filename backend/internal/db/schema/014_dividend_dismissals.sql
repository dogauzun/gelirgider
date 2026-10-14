-- Expected dividends the user dismissed (ADR 0009). User data, so it has
-- user_id. date is the dividend ex-date. portfolio.Repo.Delete deletes a
-- portfolio's rows along with it. No undo for now.
CREATE TABLE dividend_dismissals (
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    date TEXT NOT NULL,
    PRIMARY KEY (portfolio_id, instrument_id, date)
);
