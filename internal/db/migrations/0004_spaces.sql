-- Spaces: every warranty, tag and document belongs to a space, and users see
-- the spaces they are members of. Each user has a personal space, and there
-- is one household space everyone belongs to. More (custom) spaces can be
-- added later without changing this model.
CREATE TABLE spaces (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('personal', 'household', 'custom')),
    owner_id   INTEGER REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX spaces_one_personal ON spaces (owner_id) WHERE kind = 'personal';
CREATE UNIQUE INDEX spaces_one_household ON spaces (kind) WHERE kind = 'household';

CREATE TABLE space_members (
    space_id INTEGER NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (space_id, user_id)
);
CREATE INDEX space_members_user ON space_members (user_id);

INSERT INTO spaces (name, kind, owner_id) SELECT 'Personal', 'personal', id FROM users;
INSERT INTO space_members (space_id, user_id) SELECT id, owner_id FROM spaces WHERE kind = 'personal';
INSERT INTO spaces (name, kind) VALUES ('Household', 'household');
INSERT INTO space_members (space_id, user_id)
    SELECT (SELECT id FROM spaces WHERE kind = 'household'), id FROM users;

-- Existing warranties start out personal; user_id now means "added by".
ALTER TABLE warranties ADD COLUMN space_id INTEGER REFERENCES spaces (id) ON DELETE CASCADE;
UPDATE warranties SET space_id = (SELECT id FROM spaces WHERE kind = 'personal' AND owner_id = warranties.user_id);
CREATE INDEX warranties_space ON warranties (space_id, expires_on);

-- Tags belong to a space, unique by name within it.
CREATE TABLE tags_new (
    id       INTEGER PRIMARY KEY,
    space_id INTEGER NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name     TEXT NOT NULL COLLATE NOCASE,
    color    TEXT NOT NULL,
    UNIQUE (space_id, name)
);
INSERT INTO tags_new (id, space_id, name, color)
    SELECT t.id, s.id, t.name, t.color FROM tags t
    JOIN spaces s ON s.kind = 'personal' AND s.owner_id = t.user_id;
DROP TABLE tags;
ALTER TABLE tags_new RENAME TO tags;

-- Documents remember who added them: Paperless documents are opened with
-- that person's Paperless connection.
ALTER TABLE attachments ADD COLUMN added_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
UPDATE attachments SET added_by = (SELECT user_id FROM warranties WHERE id = attachments.warranty_id);

-- Reminders are per person, so everyone in a shared space gets theirs.
CREATE TABLE reminders_sent_new (
    user_id        INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    warranty_id    INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    threshold_days INTEGER NOT NULL,
    expires_on     TEXT NOT NULL,
    sent_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, warranty_id, threshold_days, expires_on)
);
INSERT INTO reminders_sent_new (user_id, warranty_id, threshold_days, expires_on, sent_at)
    SELECT w.user_id, r.warranty_id, r.threshold_days, r.expires_on, r.sent_at
    FROM reminders_sent r JOIN warranties w ON w.id = r.warranty_id;
DROP TABLE reminders_sent;
ALTER TABLE reminders_sent_new RENAME TO reminders_sent;
