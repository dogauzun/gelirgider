-- An import id is in URLs (?import=, ?import_id=) and both an import's and a
-- transaction's id are held by open pages, so a deleted one must never be
-- handed out again: a stale tab's undo, edit or delete would hit the newer
-- row (GLG-1009, as GLG-888 did for portfolios in 027). Both tables are
-- rebuilt the 027 way, every row keeping its id; copying the rows in sets
-- each sequence to the highest id present now. Columns keep their order,
-- including the ones 010 and 018 added with ALTER TABLE.
--
-- transactions.import_id is the only reference to statement_imports and
-- nothing references transactions, so transactions is dropped first and
-- filled last. defer_foreign_keys still moves every check to COMMIT, as in
-- 027.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE statement_imports_copy AS
    SELECT id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency
    FROM statement_imports;
CREATE TABLE transactions_copy AS
    SELECT id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id
    FROM transactions;

DROP TABLE transactions;
DROP TABLE statement_imports;

CREATE TABLE statement_imports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    broker TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    file_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL,
    currency TEXT,
    UNIQUE (user_id, portfolio_id, file_sha256)
);

CREATE TABLE transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    instrument_id INTEGER REFERENCES instruments(id),
    type TEXT NOT NULL,
    date TEXT NOT NULL,
    quantity TEXT,
    price TEXT,
    amount TEXT NOT NULL,
    fee TEXT NOT NULL,
    currency TEXT NOT NULL,
    note TEXT,
    created_at TEXT NOT NULL,
    import_id INTEGER REFERENCES statement_imports(id)
);

CREATE INDEX idx_transactions_user_portfolio_date ON transactions (user_id, portfolio_id, date);
CREATE INDEX idx_transactions_import ON transactions (import_id);

INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency)
    SELECT id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency
    FROM statement_imports_copy;
INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id)
    SELECT id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id
    FROM transactions_copy;

DROP TABLE transactions_copy;
DROP TABLE statement_imports_copy;
