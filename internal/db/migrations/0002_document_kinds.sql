-- Receipts and invoices are one type: proof of purchase.
UPDATE attachments SET kind = 'purchase' WHERE kind IN ('receipt', 'invoice');
