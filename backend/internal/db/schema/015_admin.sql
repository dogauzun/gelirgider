-- System admin flag (ADR 0010). An admin is a normal user with this flag set;
-- auth.RequireAdmin reads it from here on every request.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
