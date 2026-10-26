-- The language picked in settings ('' = follow the browser), and the
-- browser's language as last seen, which reminders use in that case.
ALTER TABLE user_settings ADD COLUMN language TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN browser_language TEXT NOT NULL DEFAULT '';
