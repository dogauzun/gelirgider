-- The BIST bulletins of 21 May to 9 June 2020 print the trade date as
-- day.month.year ("21.05.2020", "1.06.2020"), and the fetcher stored that
-- text as the price date, which no YYYY-MM-DD query ever matches (GLG-783).
-- Convert each such row to its ISO date, or drop it when a row for that
-- instrument and ISO date already exists.
--
-- Zero-pad the day, then the month, so every row is DD.MM.YYYY.
UPDATE prices SET date = '0' || date WHERE date LIKE '_.%.____';
UPDATE prices SET date = SUBSTR(date, 1, 3) || '0' || SUBSTR(date, 4)
    WHERE date LIKE '__._.____';

DELETE FROM prices
    WHERE date LIKE '__.__.____'
    AND EXISTS (SELECT 1 FROM prices iso
        WHERE iso.instrument_id = prices.instrument_id
        AND iso.date = SUBSTR(prices.date, 7, 4) || '-' || SUBSTR(prices.date, 4, 2) || '-' || SUBSTR(prices.date, 1, 2));

UPDATE prices
    SET date = SUBSTR(date, 7, 4) || '-' || SUBSTR(date, 4, 2) || '-' || SUBSTR(date, 1, 2)
    WHERE date LIKE '__.__.____';
