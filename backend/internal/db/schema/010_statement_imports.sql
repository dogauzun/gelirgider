-- One row per imported broker statement file. Only the period and the file
-- hash are kept: never the customer name, TCKN, address, IBAN or any other
-- personal field from the statement. The hash is what makes re-importing the
-- same file into the same portfolio a duplicate.
CREATE TABLE statement_imports (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER NOT NULL REFERENCES portfolios(id),
    broker TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    file_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (user_id, portfolio_id, file_sha256)
);

-- NULL for transactions entered by hand, set to the import that created them
-- otherwise, so deleting an import can take its transactions with it.
ALTER TABLE transactions ADD COLUMN import_id INTEGER REFERENCES statement_imports(id);

CREATE INDEX idx_transactions_import ON transactions (import_id);
