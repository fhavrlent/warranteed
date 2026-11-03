# Changelog

## v0.7.0 — 2026-11-03

### Repairs and claims

- A **Claims** card on each warranty records what went wrong, when it was reported, its status (open, repaired, replaced, refunded or rejected), when it was resolved, the shop's case or RMA number, what you paid, and notes.
- Claims hold their own documents, uploaded or linked from Paperless. They also show among the warranty's documents, and stay there if the claim is deleted.
- A warranty with an open claim is marked in the list.
- On a warranty linked to HomeBox, each claim becomes an entry in the item's maintenance log, kept up to date as the claim changes. Claims made before linking are copied when you link.

### Quantities and splitting

- A warranty can cover several pieces bought together. The price is per piece, and the total counts towards covered value and sorting.
- Claude reads the quantity from receipts. Paperless custom fields can give it too, with the `quantity` key of `PAPERLESS_DRAFT_FIELDS`.
- HomeBox items, CSV export and CSV import carry the quantity.
- **Split off pieces** moves some of a warranty's pieces, with their serial numbers, to a warranty of their own, for when one is sold or lent. The new one keeps the purchase and coverage details, tags and documents. Claims stay with the original.
- On a warranty linked to HomeBox, the item can be split too. The new warranty gets its own item in the same place, and the original item's quantity, serial number and notes follow what's left.

### Void warranties

- **Mark as void**, with an optional reason, is for a warranty that no longer applies though you still have the product, for example after a drop or a repair somewhere else.
- A void warranty sends no reminders and doesn't count towards covered value. It has its own filter, and **Make valid again** undoes it.
- CSV export and import have a `void_reason` column.

### Calendar

- **Settings → Calendar** gives you a private link to subscribe to in Apple, Google or Outlook Calendar. Each warranty you can see appears on the day it ends.
- You can get a new link, which stops the old one working, or turn the link off.

### HomeBox

- Product photos are copied to the HomeBox item, including ones added later. They can also be stored only in HomeBox and shown from there.
- **Link existing item** finds locations as well as items.
- **Add to HomeBox** can put an item inside another item that holds things, such as a box or an organizer.
- A receipt in Paperless whose warranties are linked to several HomeBox items now links to all of those items.

### Drafts from Paperless

- Claude reads both the date of sale and the date the goods were received. **Count warranties from** picks which one a draft starts on, and a draft with two dates lets you switch.
- An extended warranty bought with a product lengthens that product's warranty instead of becoming a draft of its own.
- **Make drafts of** chooses between products worth a warranty and every product. A document gives up to 30 drafts.

### List

- The list keeps its filter while the browser tab is open.
- Long product names are shown in full.
- The sort order can be reversed.
- A Paperless document attached to several warranties links to a list of just those warranties.
- Product photos are shown as small previews, so the list loads much less data.

### Upgrading

Database migrations 0011 to 0017 run automatically on startup.

If a reverse proxy asks people to sign in (Authelia, an Authentik outpost and the like), let `/calendar/` through without it. Calendar apps can't sign in, and calendar links carry their own secret.

## v0.6.0 — 2026-11-02

Drafts from new documents in Paperless, read from the document, from custom fields or by Claude, and links back from Paperless documents to their warranties and HomeBox items. See the [release notes](https://github.com/fhavrlent/warranteed/releases/tag/v0.6.0).

## v0.5.0 — 2026-11-01

HomeBox: add warranties to your inventory or link existing items. See the [release notes](https://github.com/fhavrlent/warranteed/releases/tag/v0.5.0).
