-- Warranty claims: something went wrong and was reported to the shop or
-- maker. homebox_entry is the claim's entry in the HomeBox item's
-- maintenance log, once copied there.
CREATE TABLE claims (
    id            INTEGER PRIMARY KEY,
    warranty_id   INTEGER NOT NULL REFERENCES warranties (id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open',
    reported_on   TEXT NOT NULL,
    resolved_on   TEXT,
    case_number   TEXT NOT NULL DEFAULT '',
    cost          REAL,
    notes         TEXT NOT NULL DEFAULT '',
    homebox_entry TEXT NOT NULL DEFAULT '',
    added_by      INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX claims_warranty ON claims (warranty_id);

-- A document can belong to a claim; deleting the claim keeps the document
-- on the warranty.
ALTER TABLE attachments ADD COLUMN claim_id INTEGER REFERENCES claims (id) ON DELETE SET NULL;
