-- Shared market reference data (not user-scoped): benchmark series that
-- are not FX rates, e.g. BIST 100 daily closes (XU100) and monthly CPI
-- levels (TUFE). For TUFE, date is the first day of the index's month.
-- value is exact decimal text.
CREATE TABLE benchmark_values (
    code TEXT NOT NULL,
    date TEXT NOT NULL,
    value TEXT NOT NULL,
    source TEXT NOT NULL,
    PRIMARY KEY(code, date)
);
