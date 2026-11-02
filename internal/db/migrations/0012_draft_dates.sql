-- The dates a draft's document gives for the sale and for when the goods
-- were received, when they differ, so the owner can pick one.
ALTER TABLE warranties ADD COLUMN date_sold TEXT NOT NULL DEFAULT '';
ALTER TABLE warranties ADD COLUMN date_received TEXT NOT NULL DEFAULT '';

-- Which of those dates a user's drafts count warranties from: 'received' or
-- 'sold'.
ALTER TABLE user_settings ADD COLUMN warranty_start TEXT NOT NULL DEFAULT 'received';
