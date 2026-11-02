-- The HomeBox item a product photo was last sent to, so it isn't sent twice.
-- For a photo stored in HomeBox it is the item that holds it, and
-- homebox_attachment is the photo there.
ALTER TABLE attachments ADD COLUMN homebox_item TEXT NOT NULL DEFAULT '';
ALTER TABLE attachments ADD COLUMN homebox_attachment TEXT NOT NULL DEFAULT '';

-- Store product photos in HomeBox by default, on warranties linked to an item.
ALTER TABLE user_settings ADD COLUMN homebox_photos_default INTEGER NOT NULL DEFAULT 0;
