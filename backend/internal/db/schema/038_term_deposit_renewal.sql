-- The deposit a term deposit renews: renewed_from is the id of the matured
-- deposit whose principal and net interest opened this one, NULL for a deposit
-- opened on its own. The unique index lets a deposit be renewed once (NULLs
-- are distinct, so deposits that renew nothing do not collide).
ALTER TABLE term_deposits ADD COLUMN renewed_from INTEGER REFERENCES term_deposits(id);

CREATE UNIQUE INDEX idx_term_deposits_renewed_from ON term_deposits (renewed_from);
