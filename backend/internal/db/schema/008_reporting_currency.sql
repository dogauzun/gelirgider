-- Currency the summary and history APIs convert positions and cash into for
-- display. Storage stays in each row's own currency; see ADR 0003.
ALTER TABLE users ADD COLUMN reporting_currency TEXT NOT NULL DEFAULT 'TRY';
