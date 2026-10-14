-- price.Repo.HasSource asks "is there any row for this date and source?"
-- once per weekday of every backfill walk. The (instrument_id, date) primary
-- key cannot answer that, so each check scanned the whole prices table
-- (GLG-691). empty_fetches needs nothing: its (source, date) key serves the
-- same check.
CREATE INDEX idx_prices_date_source ON prices (date, source);
