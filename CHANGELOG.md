# Changelog

## v0.10.0 — 2026-11-21

### Forms

- Every field is tied to its label, and its hint or error is read out with it, for screen readers and voice control.
- Saving a form with mistakes moves focus to the first field that needs fixing.
- On phones and tablets, fields use larger text, so iOS no longer zooms in when you tap one.

### Theme

- The **System** theme now works even before the app's scripts load, and **Settings → Display** says whether the system is in light or dark mode right now.
- Changing the theme in one tab changes it in the other open tabs too.
- Notifications follow the theme you picked, not only the system's.

### Dialogs

- Dialogs use the browser's built-in modal dialog. Esc and the back gesture close them, clicking outside closes them, and focus goes back to the button that opened them.
- Scrolling over the dark area around a dialog no longer moves the page behind it, and the page's scrollbar no longer disappears while a dialog is open.
- A delete confirmation starts on **Cancel**, so pressing Enter doesn't delete anything.
- Notifications and date pickers shown while a dialog is open can be clicked.

### Upgrading

Nothing to do. Colours now rely on CSS `light-dark()`, available in browsers since 2024 (Chrome 123, Firefox 120, Safari 17.5).

## v0.9.0 — 2026-11-18

### Wherehouse: several households

- Warranteed can connect to more than one Wherehouse household, say a cottage as well as home, each with its own connection. Tick the households on Wherehouse's connect page.
- **Settings → Wherehouse** lists each connected household with its own **Disconnect**, and **Choose households** connects more. Households not connected yet, new ones included, are listed with a **Connect** button.
- **Add to Wherehouse**, **Link existing thing** and confirming a draft ask which household when there are several, starting with the last one picked. The warranty shows which household its thing is in.
- A household Warranteed lost access to can be connected again, and Wherehouse replaces the old token.
- Disconnecting a household unlinks only its own warranties.
- A household deleted in Wherehouse, or one you left, is disconnected on its own. Its warranties are unlinked unless someone else here is still connected to it.

### Settings

- Turning off the calendar link, and disconnecting Paperless or HomeBox, now ask first.

### Upgrading

Database migration 0026 runs automatically on startup. It moves the existing Wherehouse connection into the new list of connections, and Warranteed fills in its household at the first sync.

Several households need Wherehouse v0.2.0 or later. With an older Wherehouse, Warranteed connects one household as before.

## v0.8.0 — 2026-11-17

### Wherehouse

- Warranties can be part of a [Wherehouse](https://github.com/fhavrlent/wherehouse) inventory. Set `WHEREHOUSE_URL`, then each person clicks **Connect to Wherehouse** under **Settings → Wherehouse** and allows it there. There's no key to copy.
- **Add to Wherehouse** makes a thing for a warranty, in a place you pick or nowhere yet. **Link existing thing** finds one already there. Confirming a draft can add its product straight away.
- The name, brand, model, serial numbers, quantity and notes are kept in step both ways. The purchase and warranty details stay in Warranteed, and Wherehouse shows them read-only.
- Documents in Paperless, claims' documents included, are on both. Adding, deleting, renaming or changing the type of one on either side does the same on the other.
- Product photos are copied to the thing. With **Keep product photos only in Wherehouse** on, they are the thing's photos instead, kept only there and shown from there.
- Claims are entries in the thing's maintenance log. Editing or deleting an entry there changes or deletes the claim.
- The warranty shows where the thing is and its label code, which you can search for. Selling or archiving the thing archives the warranty.
- Wherehouse pings Warranteed when something changes, and Warranteed also checks every 10 minutes. Changes Wherehouse couldn't take while it was down are sent once it's back, after a restart too.
- A thing belongs to one warranty. A warranty set to **Only me** stays out of a Wherehouse household others share.
- Splitting off pieces of a linked warranty can make a thing for them too.
- **Disconnect** also removes Warranteed's access in Wherehouse.

### Upgrading

Database migrations 0018 to 0025 run automatically on startup. If several warranties were linked to the same Wherehouse thing, the oldest keeps it and the others are unlinked.

Connecting needs a Wherehouse that supports PKCE and gives connected apps their own limited access. Wherehouse pings only public addresses and the hosts listed in its `WHEREHOUSE_HOOK_ALLOWED_HOSTS`. If it can't reach Warranteed, changes still arrive within 10 minutes.

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
