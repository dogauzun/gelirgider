-- Shared market reference data (not user-scoped, ADR 0008): provider cash
-- dividends per instrument. date is the ex-date (YYYY-MM-DD market date);
-- amount is gross cash per share as an exact decimal string, in currency
-- (the instrument's currency).
CREATE TABLE dividends (
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    date TEXT NOT NULL,
    amount TEXT NOT NULL,
    currency TEXT NOT NULL,
    source TEXT NOT NULL,
    PRIMARY KEY(instrument_id, date)
);
