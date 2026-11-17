-- What a linked warranty and its thing last agreed on, so a change can be
-- told from which side it came: for the warranty, its name, brand, model,
-- serial numbers, quantity and notes as here and as there (JSON); for a
-- document, its type and name there; for a claim, its maintenance entry
-- there (JSON). Empty until first seen.
ALTER TABLE warranties ADD COLUMN wherehouse_fields TEXT NOT NULL DEFAULT '';
ALTER TABLE attachments ADD COLUMN wherehouse_seen TEXT NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN wherehouse_seen TEXT NOT NULL DEFAULT '';
