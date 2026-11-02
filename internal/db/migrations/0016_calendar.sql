-- The secret in a user's calendar feed link, or NULL when they have none.
ALTER TABLE users ADD COLUMN calendar_token TEXT;
CREATE UNIQUE INDEX users_calendar_token ON users (calendar_token) WHERE calendar_token IS NOT NULL;
