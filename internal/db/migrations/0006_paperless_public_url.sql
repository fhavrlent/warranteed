-- Address the browser uses to open Paperless, when paperless_url is an
-- internal one ('' = same as the URL the server uses).
ALTER TABLE user_settings ADD COLUMN paperless_public_url TEXT NOT NULL DEFAULT '';
