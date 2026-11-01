-- Whether Warranteed writes links to the warranty and the HomeBox item into
-- custom fields of the user's documents in Paperless.
ALTER TABLE user_settings ADD COLUMN paperless_backlinks INTEGER NOT NULL DEFAULT 0;
