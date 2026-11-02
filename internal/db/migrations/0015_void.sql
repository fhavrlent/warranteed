-- A void warranty no longer covers the product (damage, an unauthorised
-- repair, a broken seal) though it hasn't expired; why, if the user says.
ALTER TABLE warranties ADD COLUMN voided_at TEXT;
ALTER TABLE warranties ADD COLUMN void_reason TEXT NOT NULL DEFAULT '';
