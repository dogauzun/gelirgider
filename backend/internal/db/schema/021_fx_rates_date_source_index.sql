-- fx.Repo.HasSource asks "is there any row for this date and source?" once
-- per weekday of every TCMB backfill walk. The (currency, date) primary key
-- cannot answer that, so each check scanned the whole fx_rates table
-- (GLG-692). empty_fetches needs nothing: its (source, date) key serves the
-- same check.
CREATE INDEX idx_fx_rates_date_source ON fx_rates (date, source);
