-- Profile picture downloaded at sign-in and served by the app, so the page
-- needn't load images from the identity provider. picture_etag changes with
-- the image ('' = none).
ALTER TABLE users ADD COLUMN picture_data BLOB;
ALTER TABLE users ADD COLUMN picture_type TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN picture_etag TEXT NOT NULL DEFAULT '';
