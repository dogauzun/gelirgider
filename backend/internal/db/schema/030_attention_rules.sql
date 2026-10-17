-- Per-user overrides of the attention rules, as a JSON object holding only
-- what the user changed; '{}' means every rule at its default. Parsed and
-- validated in Go (auth.attentionRules), never in SQL.
ALTER TABLE users ADD COLUMN attention_rules TEXT NOT NULL DEFAULT '{}';
