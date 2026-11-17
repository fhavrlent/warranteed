-- A Wherehouse thing belongs to one warranty. Where several are linked to
-- the same thing, the first keeps it and the others are unlinked here.
UPDATE claims SET wherehouse_entry = NULL, wherehouse_seen = ''
WHERE warranty_id IN (SELECT w.id FROM warranties w WHERE w.wherehouse_thing IS NOT NULL
    AND w.id > (SELECT MIN(o.id) FROM warranties o WHERE o.wherehouse_thing = w.wherehouse_thing));
UPDATE warranties SET wherehouse_thing = NULL, wherehouse_label = '', wherehouse_path = '', wherehouse_fields = ''
WHERE wherehouse_thing IS NOT NULL
    AND id > (SELECT MIN(o.id) FROM warranties o WHERE o.wherehouse_thing = warranties.wherehouse_thing);
DROP INDEX warranties_wherehouse;
CREATE UNIQUE INDEX warranties_wherehouse ON warranties (wherehouse_thing) WHERE wherehouse_thing IS NOT NULL;

-- A linked warranty's purchase and warranty details waiting to be sent to
-- its thing, kept until Wherehouse took them. seq grows with each change,
-- so a change made while sending isn't taken as sent; next_at is when to
-- try again after a failure.
CREATE TABLE wherehouse_pushes (
    warranty_id INTEGER PRIMARY KEY REFERENCES warranties (id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL DEFAULT 1,
    attempts    INTEGER NOT NULL DEFAULT 0,
    next_at     TEXT NOT NULL
);
