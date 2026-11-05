-- A product photo kept only in Wherehouse: wherehouse_thing is the thing
-- that holds it and wherehouse_photo is the photo there.
ALTER TABLE attachments ADD COLUMN wherehouse_photo INTEGER;

-- Keep product photos only in Wherehouse, on warranties linked to a thing.
ALTER TABLE user_settings ADD COLUMN wherehouse_photos_only INTEGER NOT NULL DEFAULT 0;
