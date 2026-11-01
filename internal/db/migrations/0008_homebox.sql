-- The HomeBox item a warranty is linked to ('' = none) and its asset ID, as
-- shown on HomeBox labels.
ALTER TABLE warranties ADD COLUMN homebox_id TEXT NOT NULL DEFAULT '';
ALTER TABLE warranties ADD COLUMN homebox_asset_id TEXT NOT NULL DEFAULT '';
-- Each user's own HomeBox API key.
ALTER TABLE user_settings ADD COLUMN homebox_token TEXT NOT NULL DEFAULT '';
