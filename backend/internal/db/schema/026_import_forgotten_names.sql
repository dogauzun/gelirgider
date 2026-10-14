-- A fund name a committed Garanti fund statement printed for two funds, with
-- that statement's period end as a DATE (GLG-821). User data, keyed like
-- import_aliases. A statement whose period ends before it does not learn the
-- name again; one of the same or a later period end removes it and learns
-- under the import_aliases rule of GLG-773. A later forget moves it forward.
CREATE TABLE import_forgotten_names (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    broker TEXT NOT NULL,
    symbol TEXT NOT NULL,
    period_end TEXT NOT NULL,
    UNIQUE (user_id, broker, symbol)
);
