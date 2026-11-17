-- Set when a warranty was archived because its thing was sold or archived
-- in Wherehouse, so it comes back if that's undone there.
ALTER TABLE warranties ADD COLUMN wherehouse_archived INTEGER NOT NULL DEFAULT 0;

-- The Wherehouse thing a photo was sent to, so it isn't sent twice.
ALTER TABLE attachments ADD COLUMN wherehouse_thing INTEGER;
