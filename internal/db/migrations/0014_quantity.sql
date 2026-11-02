-- How many pieces of the product a warranty covers, bought together.
ALTER TABLE warranties ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;
