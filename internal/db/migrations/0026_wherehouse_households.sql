-- A person can be connected to several Wherehouse households, each with
-- its own token, ping secret and change cursor. household_id is 0 for a
-- connection made before households were recorded, until it's asked.
CREATE TABLE wherehouse_connections (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    household_id INTEGER NOT NULL DEFAULT 0,
    household    TEXT    NOT NULL DEFAULT '',
    token        TEXT    NOT NULL,
    hook_secret  TEXT    NOT NULL DEFAULT '',
    since        TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX wherehouse_connections_household ON wherehouse_connections (user_id, household_id)
    WHERE household_id != 0;

INSERT INTO wherehouse_connections (user_id, household, token, hook_secret, since)
SELECT user_id, wherehouse_household, wherehouse_token, wherehouse_hook_secret, wherehouse_since
FROM user_settings WHERE wherehouse_token != '';
UPDATE user_settings SET wherehouse_token = '', wherehouse_household = '', wherehouse_hook_secret = '',
    wherehouse_since = '';

-- The household a linked thing is in, so it's reached through a
-- connection to that household; NULL until known.
ALTER TABLE warranties ADD COLUMN wherehouse_household INTEGER;
