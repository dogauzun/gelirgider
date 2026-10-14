-- Shared market reference data (not user-scoped, ADR 0008): stock splits
-- and bonus issues per instrument. date is the ex-date (YYYY-MM-DD market
-- date); ratio is new shares per old share as an exact decimal string.
CREATE TABLE splits (
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    date TEXT NOT NULL,
    ratio TEXT NOT NULL,
    source TEXT NOT NULL,
    PRIMARY KEY(instrument_id, date)
);
