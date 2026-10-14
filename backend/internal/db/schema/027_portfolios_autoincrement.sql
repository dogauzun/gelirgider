-- A portfolio id is in its URLs (/portfolios/:id/...), so a deleted
-- portfolio's id must never be handed out again (GLG-888). AUTOINCREMENT can
-- only be declared by CREATE TABLE, so the table is rebuilt with every row
-- keeping its id. Copying the rows in sets the sequence to the highest id
-- present now; a higher id deleted before this file ran is not known.
--
-- The runner applies this file in one transaction with foreign keys on, and
-- PRAGMA foreign_keys cannot change inside a transaction. Dropping portfolios
-- deletes its rows first, which the references from transactions,
-- statement_imports and dividend_dismissals (all NO ACTION, none CASCADE)
-- refuse at once. defer_foreign_keys moves that check to COMMIT, by which
-- time the same ids are back, so child rows are never touched; it switches
-- itself off at COMMIT or ROLLBACK. The old table is not renamed out of the
-- way because SQLite would repoint those references to the new name.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE portfolios_copy AS SELECT id, user_id, name, created_at FROM portfolios;

DROP TABLE portfolios;

CREATE TABLE portfolios (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);

INSERT INTO portfolios (id, user_id, name, created_at)
    SELECT id, user_id, name, created_at FROM portfolios_copy;

DROP TABLE portfolios_copy;
