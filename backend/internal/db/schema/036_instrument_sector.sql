-- The sector of an instrument (ADR 0026).
-- sector: the provider's raw string, never mapped when stored. NULL means not
-- fetched yet, an empty string means fetched and the provider has none.
-- sector_override: a sector key, written only by the admin route; it wins over
-- the key the raw string maps to.
ALTER TABLE instruments ADD COLUMN sector TEXT;
ALTER TABLE instruments ADD COLUMN sector_override TEXT;
