-- Shared market reference data (not user-scoped): closing prices per
-- instrument per date. "manual" is the only source for now; a later
-- ticket adds automated providers that write the same table.
CREATE TABLE prices (
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    date TEXT NOT NULL,
    close TEXT NOT NULL,
    source TEXT NOT NULL,
    PRIMARY KEY(instrument_id, date)
);
