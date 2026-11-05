-- Set when someone unlinks a warranty from Wherehouse, so it isn't linked
-- again through its HomeBox item.
ALTER TABLE warranties ADD COLUMN wherehouse_unlinked INTEGER NOT NULL DEFAULT 0;
