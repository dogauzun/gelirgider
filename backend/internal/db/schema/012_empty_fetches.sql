-- Shared market reference data (not user-scoped): dates a date-scoped
-- source (bist_bulletin, tcmb) was fetched for and had no data, i.e. market
-- holidays, so the backfill does not request them again on every start
-- (GLG-277). HasSource treats a row here as already synced.
CREATE TABLE empty_fetches (
    source TEXT NOT NULL,
    date TEXT NOT NULL,
    PRIMARY KEY(source, date)
);
