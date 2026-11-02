-- Which products on a document Claude makes drafts of: 'worthy' (ones worth
-- tracking a warranty for) or 'all'.
ALTER TABLE user_settings ADD COLUMN draft_items TEXT NOT NULL DEFAULT 'worthy';
