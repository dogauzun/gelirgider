-- A user id is held by the admin page for "make admin", "reset password" and
-- "delete", so a deleted user's id must never be handed out again: a stale
-- tab's action would hit the newer account (GLG-1013, as GLG-888 and
-- GLG-1009 did in 027 and 028). The table is rebuilt the 027 way, every row
-- keeping its id; copying the rows in sets the sequence to the highest id
-- present now. Columns keep their order, including reporting_currency (008)
-- and is_admin (015), added with ALTER TABLE.
--
-- users is referenced by sessions, portfolios, transactions, manual_prices,
-- statement_imports, dividend_dismissals, import_aliases and
-- import_forgotten_names, all NO ACTION: no CASCADE, SET NULL or SET DEFAULT
-- in any schema file, and no trigger. So the implicit DELETE of DROP TABLE
-- touches no child row; defer_foreign_keys only moves the violation check
-- to COMMIT, by which time the same ids are back, as in 027.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE users_copy AS
    SELECT id, email, password_hash, created_at, reporting_currency, is_admin
    FROM users;

DROP TABLE users;

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    reporting_currency TEXT NOT NULL DEFAULT 'TRY',
    is_admin INTEGER NOT NULL DEFAULT 0
);

INSERT INTO users (id, email, password_hash, created_at, reporting_currency, is_admin)
    SELECT id, email, password_hash, created_at, reporting_currency, is_admin
    FROM users_copy;

DROP TABLE users_copy;
