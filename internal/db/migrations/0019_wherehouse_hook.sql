-- The secret Wherehouse signs its pings with, for each person's connection.
ALTER TABLE user_settings ADD COLUMN wherehouse_hook_secret TEXT NOT NULL DEFAULT '';
