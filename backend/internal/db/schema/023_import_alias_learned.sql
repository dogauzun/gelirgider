-- A fund name learned from the balance table of a committed Garanti fund
-- statement, next to the code it printed (GLG-766), rather than the user's own
-- pick. 1 for a learned name, 0 for a pick. A pick replaces a learned name of
-- the same key, a learned name never replaces a pick, a statement that prints
-- the name for two funds removes a learned one, and a later preview resolves a
-- fund name with no pick through the learned names by prefix.
ALTER TABLE import_aliases ADD COLUMN learned INTEGER NOT NULL DEFAULT 0;
