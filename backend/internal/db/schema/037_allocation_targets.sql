-- The target weight of one asset class for one user, in one portfolio. weight
-- is exact decimal text, a fraction (0.4 for 40%). portfolio_id NULL is the
-- set for all portfolios together; SQLite treats NULLs as distinct in a
-- unique index, so that set has its own partial index.
CREATE TABLE allocation_targets (
    user_id INTEGER NOT NULL REFERENCES users(id),
    portfolio_id INTEGER REFERENCES portfolios(id),
    asset_class TEXT NOT NULL,
    weight TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_allocation_targets_portfolio ON allocation_targets (user_id, portfolio_id, asset_class);
CREATE UNIQUE INDEX idx_allocation_targets_all ON allocation_targets (user_id, asset_class) WHERE portfolio_id IS NULL;
