-- Wherehouse: the thing a warranty belongs to there, with its label code
-- and where it is, as last heard, for showing and searching here.
ALTER TABLE warranties ADD COLUMN wherehouse_thing INTEGER;
ALTER TABLE warranties ADD COLUMN wherehouse_label TEXT NOT NULL DEFAULT '';
ALTER TABLE warranties ADD COLUMN wherehouse_path TEXT NOT NULL DEFAULT '';
CREATE INDEX warranties_wherehouse ON warranties (wherehouse_thing) WHERE wherehouse_thing IS NOT NULL;

-- A claim's entry in the thing's maintenance log there.
ALTER TABLE claims ADD COLUMN wherehouse_entry INTEGER;

-- Each person's Wherehouse connection: the token from connecting, the
-- household it works in, and how far its change feed was read.
ALTER TABLE user_settings ADD COLUMN wherehouse_token TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN wherehouse_household TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN wherehouse_since TEXT NOT NULL DEFAULT '';
