-- A document on a warranty linked to a Wherehouse thing: wherehouse_thing
-- is that thing and wherehouse_attachment the same document on it, so a
-- document removed in one app goes from the other too. Kept only in
-- Wherehouse (storage 'wherehouse'), it is the file there.
ALTER TABLE attachments ADD COLUMN wherehouse_attachment INTEGER;
