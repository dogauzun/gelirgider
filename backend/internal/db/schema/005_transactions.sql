CREATE TABLE transactions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    instrument_id INTEGER REFERENCES instruments(id),
    type TEXT NOT NULL,
    date TEXT NOT NULL,
    quantity TEXT,
    price TEXT,
    amount TEXT NOT NULL,
    fee TEXT NOT NULL,
    currency TEXT NOT NULL,
    note TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_transactions_user_portfolio_date ON transactions (user_id, portfolio_id, date);
