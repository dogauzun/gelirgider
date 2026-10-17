-- The stored asset class of an instrument: one of equity, fund or commodity,
-- written by the symbol seed. NULL means no class is stored and the class is
-- derived from the market (ADR 0026).
ALTER TABLE instruments ADD COLUMN asset_class TEXT;
