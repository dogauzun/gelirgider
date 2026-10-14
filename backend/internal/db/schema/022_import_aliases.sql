-- The user's own instrument pick for a name a statement printed (GLG-735):
-- a Garanti fund movement names its fund, not its code, and the same name
-- would ask for the same pick in every later statement. User data, keyed by
-- the broker and the row's symbol exactly as the parser emits it. The
-- preview uses a pick only when the symbol resolves to no instrument.
CREATE TABLE import_aliases (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    broker TEXT NOT NULL,
    symbol TEXT NOT NULL,
    instrument_id INTEGER NOT NULL REFERENCES instruments(id),
    created_at TEXT NOT NULL,
    UNIQUE (user_id, broker, symbol)
);
