-- Warranties created from new documents in Paperless start as drafts, kept
-- out of totals and reminders until someone checks and confirms them.
ALTER TABLE warranties ADD COLUMN draft INTEGER NOT NULL DEFAULT 0;
-- Set by every save, so details found later never overwrite someone's edits.
ALTER TABLE warranties ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
-- Whether a user gets drafts, and where their details come from: 'off' (no
-- drafts), 'paperless' (the document's own data), 'fields' (Paperless custom
-- fields, e.g. filled by paperless-gpt) or 'claude'.
ALTER TABLE user_settings ADD COLUMN draft_source TEXT NOT NULL DEFAULT 'off';
