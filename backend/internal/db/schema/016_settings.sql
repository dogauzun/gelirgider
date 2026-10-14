-- Install-wide key/value config (registration toggle, provider keys,
-- scheduler settings). Not user data, so no user_id: a deliberate exception
-- like prices/splits. Secrets are stored AES-GCM encrypted by
-- internal/settings. updated_at is UTC RFC3339.
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
