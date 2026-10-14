-- Per-user manual price overrides. Unlike prices (shared fetched data),
-- each user's manual entry is visible only to them and never overwrites
-- another user's entry or a fetched price for the same instrument/date.
CREATE TABLE manual_prices (
    user_id INTEGER NOT NULL REFERENCES users(id),
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    date TEXT NOT NULL,
    close TEXT NOT NULL,
    PRIMARY KEY (user_id, instrument_id, date)
);
