-- Stopaj (withholding tax) pre-filled on an expected dividend, per market, as
-- an exact decimal fraction in text. No row means 0. Global, like dividends: it
-- holds no user data. An operator updates a row to follow a change in law
-- (ADR 0009).
CREATE TABLE stopaj_rates (
    market TEXT PRIMARY KEY,
    rate   TEXT NOT NULL
);
INSERT INTO stopaj_rates (market, rate) VALUES ('BIST', '0.15');
INSERT INTO stopaj_rates (market, rate) VALUES ('US', '0.20');
