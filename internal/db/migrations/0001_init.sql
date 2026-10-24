CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    issuer        TEXT NOT NULL,
    subject       TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    picture       TEXT NOT NULL DEFAULT '',
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    last_login_at TEXT,
    UNIQUE (issuer, subject)
);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    id_token     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    expires_at   TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE user_settings (
    user_id             INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    date_format         TEXT NOT NULL DEFAULT 'yyyy-MM-dd',
    currency            TEXT NOT NULL DEFAULT 'EUR',
    theme               TEXT NOT NULL DEFAULT 'system',
    expiring_soon_days  INTEGER NOT NULL DEFAULT 30,
    notify_urls         TEXT NOT NULL DEFAULT '',
    notify_days         TEXT NOT NULL DEFAULT '30,7',
    paperless_url       TEXT NOT NULL DEFAULT '',
    paperless_token     TEXT NOT NULL DEFAULT '',
    paperless_default   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE warranties (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    product_name      TEXT NOT NULL,
    brand             TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    vendor            TEXT NOT NULL DEFAULT '',
    order_number      TEXT NOT NULL DEFAULT '',
    purchase_date     TEXT NOT NULL,
    warranty_months   INTEGER,
    warranty_end_date TEXT,
    is_lifetime       INTEGER NOT NULL DEFAULT 0,
    expires_on        TEXT,
    price             REAL,
    currency          TEXT NOT NULL DEFAULT '',
    product_url       TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    archived_at       TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX warranties_user ON warranties (user_id, expires_on);

CREATE TABLE serial_numbers (
    warranty_id INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (warranty_id, position)
);

CREATE TABLE tags (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT NOT NULL COLLATE NOCASE,
    color   TEXT NOT NULL,
    UNIQUE (user_id, name)
);

CREATE TABLE warranty_tags (
    warranty_id INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    tag_id      INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (warranty_id, tag_id)
);

-- storage is 'local' or 'paperless'; status is 'ready', 'processing' or 'failed'.
CREATE TABLE attachments (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    warranty_id        INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    kind               TEXT NOT NULL,
    storage            TEXT NOT NULL,
    filename           TEXT NOT NULL,
    mime_type          TEXT NOT NULL DEFAULT 'application/octet-stream',
    size               INTEGER NOT NULL DEFAULT 0,
    local_path         TEXT NOT NULL DEFAULT '',
    paperless_doc_id   INTEGER,
    paperless_task_id  TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'ready',
    error              TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX attachments_warranty ON attachments (warranty_id);
CREATE INDEX attachments_processing ON attachments (status) WHERE status = 'processing';

-- One row per (warranty, threshold, expiry date) so each reminder fires once,
-- and fires again if the expiry date is later changed.
CREATE TABLE reminders_sent (
    warranty_id    INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    threshold_days INTEGER NOT NULL,
    expires_on     TEXT NOT NULL,
    sent_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (warranty_id, threshold_days, expires_on)
);
