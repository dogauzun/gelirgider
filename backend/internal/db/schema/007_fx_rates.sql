-- Shared market reference data (not user-scoped): TRY per 1 unit of
-- currency, per date. TRY itself has no rows (rate 1).
CREATE TABLE fx_rates (
    currency TEXT NOT NULL,
    date TEXT NOT NULL,
    rate TEXT NOT NULL,
    source TEXT NOT NULL,
    PRIMARY KEY(currency, date)
);
