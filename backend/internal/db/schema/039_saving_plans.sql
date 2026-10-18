-- A monthly standing order of one user in one portfolio. instrument_id NULL
-- is a cash deposit plan. The id is in URLs and is never reused
-- (AUTOINCREMENT, as in 032). amount is exact decimal text in currency.
-- day_of_month is 1 to 31; a day past a month's end falls on its last day.
-- Dates are YYYY-MM-DD. next_date is the oldest occurrence not yet booked or
-- skipped. end_date NULL is open-ended. fund books the amount as a cash
-- deposit before the buy.
CREATE TABLE saving_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    instrument_id INTEGER REFERENCES instruments(id),
    currency TEXT NOT NULL,
    amount TEXT NOT NULL,
    day_of_month INTEGER NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT,
    next_date TEXT NOT NULL,
    fund INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_saving_plans_user_portfolio ON saving_plans (user_id, portfolio_id);
