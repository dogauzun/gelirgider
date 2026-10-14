-- The period end of the committed statement a learned name came from
-- (GLG-773), as a DATE. A learned name replaces or forgets a learned row only
-- when its statement's period ends on the same day or later, so statements
-- committed out of period order leave the newest statement's names. NULL for
-- a pick and for a name learned before this column, which counts as older
-- than any statement.
ALTER TABLE import_aliases ADD COLUMN period_end TEXT;
