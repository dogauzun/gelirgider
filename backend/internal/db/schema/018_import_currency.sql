-- The statement's account currency (GLG-361). Midas prints a TRY and a USD
-- statement for the same month, so broker, portfolio and period alone do not
-- tell two statements apart; with the currency they do, and the commit
-- refuses a second import of the same one while the first exists. NULL for an
-- import recorded before this column, or one whose statement printed no
-- currency: it matches any currency in that check.
ALTER TABLE statement_imports ADD COLUMN currency TEXT;
