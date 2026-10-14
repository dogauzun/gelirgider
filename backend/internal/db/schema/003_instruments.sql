-- Shared market reference data (not user-scoped): tradable instruments.
CREATE TABLE instruments (
    id INTEGER PRIMARY KEY,
    symbol TEXT NOT NULL,
    market TEXT NOT NULL,
    name TEXT NOT NULL,
    currency TEXT NOT NULL,
    UNIQUE(market, symbol)
);
