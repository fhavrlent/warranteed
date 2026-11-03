# User guide

How to use Warranteed day to day. Setting up the server and its connections to Paperless-ngx, HomeBox and notification services is covered in the [README](../README.md) and the [integration guide](integrations.md).

- [Signing in](#signing-in)
- [Adding a warranty](#adding-a-warranty)
- [Documents and photos](#documents-and-photos)
- [Drafts from Paperless](#drafts-from-paperless)
- [Finding warranties](#finding-warranties)
- [Sharing with the household](#sharing-with-the-household)
- [Repairs and claims](#repairs-and-claims)
- [HomeBox](#homebox)
- [Reminders](#reminders)
- [Calendar](#calendar)
- [Void, archiving and deleting](#void-archiving-and-deleting)
- [Tags](#tags)
- [Import and export](#import-and-export)
- [Settings](#settings)

## Signing in

Click **Continue with …** and sign in with your identity provider. Warranteed has no passwords of its own. Your name, email and picture come from the provider.

## Adding a warranty

Click **Add warranty** and fill in:

- **Product**, and optionally the brand and model.
- **Purchase date**, where you bought it, the price of one piece, how many you bought and the order number.
- **Warranty**, in one of three ways:
  - **Length** in years or months, from the purchase date;
  - **End date**, when the warranty card gives a date;
  - **Lifetime**, for warranties that never expire.
- **Serial numbers**. Add as many as you need, for example for a set of several devices.
- **Tags**, a product link and notes, such as the support phone number or how to make a claim.
- **Who can see this**: the household, or only you. See [Sharing](#sharing-with-the-household).

You can drop the receipt and other documents into the same form. They're uploaded when you save.

## Documents and photos

Each warranty can hold its receipt, warranty terms and card, extended warranty, repair and claim reports, manuals, product photos and other files. You can pick several at once, with **Add documents** or **Photo**. When adding documents, you choose a type for each one, so the right file is easy to find when you make a claim.

With Paperless-ngx connected:

- Documents can be **stored in Paperless** instead of in Warranteed. The upload dialog asks where to store them. **Upload documents to Paperless by default** in Settings makes Paperless the default. Product photos are stored in Warranteed or in [HomeBox](#homebox), never in Paperless, and file types Paperless can't take stay in Warranteed.
- **Link from Paperless** attaches documents that are already there. Search by text, title or document ID.
- A document stored in Paperless shows *Paperless is processing…* until Paperless has finished with it. If an upload fails, **Check again** retries it.
- **Unlinking** a Paperless document only removes it from the warranty. The document stays in Paperless.

**Rename** changes the name shown in Warranteed; a document in Paperless keeps its title there.

## Drafts from Paperless

If your server is set up for it, receipts added to Paperless can become draft warranties, so you only check and confirm them. Drafts are off until you turn them on under **Settings → Paperless-ngx → Drafts from new documents**, where you also pick where their details come from:

| Choice | Details come from | Drafts per document |
| --- | --- | --- |
| **On, with the document's own details** | The document's title, date and correspondent in Paperless | One |
| **On, with Paperless custom fields** | Custom fields such as product, price and warranty length, filled in by paperless-gpt or by hand | One |
| **On, read by Claude** | Claude reads the receipt itself. The document is sent to Anthropic | One per device on it, or per product if you choose; up to 30 |

Which documents become drafts is decided by the Paperless workflow your admin set up, often by a tag such as `warranty`. Only documents you own in Paperless become your drafts.

When a draft arrives, the list shows a banner and the **To review** filter. On each draft:

1. Check the details and correct them with **Edit**. Anything the receipt doesn't say is left for you; a warranty length that isn't stated defaults to 24 months.
2. Click **Confirm** to make it an ordinary warranty, or delete it if you don't want to track it.

Until it's confirmed, a draft is visible only to you, sends no reminders and isn't counted in totals.

With Claude, a receipt for several devices gives a draft for each, with the shop, date, order number and receipt filled in on all of them. Delivery, discounts, food, books, music and other things nobody claims a warranty on are left out. To get a draft for every product instead, set **Make drafts of** to **Every product** in the drafts settings. Delete any draft you don't need.

Claude also:

- reads both the date of sale and the date you received the goods, when the document gives both. **Count warranties from** in the drafts settings picks which one a draft starts with: **Date received** (the default, as EU and UK law counts) or **Date of purchase** (as most US and manufacturer warranties count). A draft with two different dates lets you switch between them before you confirm it;
- adds an extended warranty or warranty insurance bought with a product to that product's warranty length, and notes it, instead of making a draft of its own.

With custom fields, the draft may be made before paperless-gpt has filled them in. Warranteed then looks again over the next ten minutes and fills the draft in, unless you've already edited it.

## Finding warranties

- **Search** looks through products, brands, models, stores, order numbers, serial numbers, tags, notes and HomeBox asset IDs. It ignores accents and allows small typos.
- **Filters**: Current, Expired, Void (when there are void warranties), Archived, All, and To review when you have drafts. With a household, you can also show only the shared warranties or only your private ones. The list remembers your filter while the browser tab is open, so it's still there when you come back from a warranty.
- From a Paperless document attached to several warranties, the **Warranty** link opens the list of just those, drafts included. **Show all** goes back to everything.
- **Sort** by expiry date, name, purchase date, price or when it was added.
- Switch between the grid and the list view.

At the top, the list shows how many warranties are covered, how many expire soon, how many have expired, and the value of what's still under warranty. What "expiring soon" means is set in Settings.

## Sharing with the household

Everyone who signs in to the same Warranteed is in one household. New warranties are shared with it by default: everyone can see and edit them, add documents and archive them.

- Choose **Only me** when adding a warranty, or **Make private** on it later, to keep it to yourself.
- Only the person who added a warranty can delete it or make it private again. Others can archive it.
- **Settings → Household → Share all with household** shares all your private warranties at once.
- Documents you attached from your Paperless can be opened by the household through your connection, but nobody sees anything else in your Paperless.

With a single user, none of this appears.

## Repairs and claims

When something breaks, **Add claim** under **Claims** on the warranty records what went wrong and when you reported it. Keep it up to date as it goes: the status (open, repaired, replaced, refunded or rejected), when it was resolved, the shop's case or RMA number, anything you paid, and notes. A warranty with an open claim is marked in the list.

A claim's menu adds documents to it, such as the shop's letter, photos of the defect or the repair report. They're stored like other documents, in Paperless if that's your default, and also appear among the warranty's documents. Deleting a claim keeps its documents on the warranty.

## HomeBox

With HomeBox connected (**Settings → HomeBox**, with an API key from HomeBox's *Profile → API keys*), a warranty can be part of your inventory:

- **Add to HomeBox** creates an item with the serial number, purchase date, price, seller and warranty details, a link to the warranty, and links to its receipts in Paperless. You can pick where it goes: a location, or an item that already holds other things, such as a box or an organizer.
- **Link existing item** searches HomeBox items and locations by name, serial number or #asset ID, so something that is also a place, like an organizer set up as a location, can be linked too. Only empty fields on the item are filled in; nothing already there changes.
- **Product photos** are copied to the item, both when adding and when linking, and so are photos you add to the warranty later. The first becomes the item's main photo unless it already has one. Each photo is copied once; deleting it in Warranteed leaves it in HomeBox. Receipts and other documents stay links to Paperless.
- **Photos stored in HomeBox:** on a linked warranty, **Photo** asks where to store new photos. **HomeBox** puts them only on the item, and Warranteed shows them from there, so there's one copy. **Store product photos in HomeBox by default** in Settings makes HomeBox the default. Removing such a photo here leaves it on the item. The household sees it through the HomeBox key of whoever added it, so it stops showing if that key expires.
- The warranty then shows the item's asset ID, the number printed on HomeBox labels, and a link to open it in HomeBox.
- **Split off pieces** can add an item for the split-off warranty next to the original and lower the original's quantity, instead of you duplicating the item in HomeBox.
- **Claims** are added to the item's maintenance log, with when they were reported and resolved, the cost, and the status and notes in the description. Changing or deleting a claim changes or removes its entry; claims made before linking are added when you link.
- **Unlink** makes the warranty forget the item. The item stays in HomeBox, with any claim entries.

HomeBox keys expire after 30 days unless you give them a later date. When yours expires, create a new one and save it in Settings.

## Reminders

Under **Settings → Reminders**, add one or more notification URLs and when to be reminded, for example `30, 7` for 30 and 7 days before a warranty expires. **Send test** checks that the URLs work. Each reminder is sent once, in your language.

Discord, Telegram, Slack, Pushover, Pushbullet, IFTTT and Join work for everyone. Self-hosted services such as ntfy, Gotify, webhooks and email need an admin to allow their server first. The [integration guide](integrations.md#reminders) has URL examples.

## Calendar

Under **Settings → Calendar**, **Create calendar link** gives you a private link to subscribe to in Apple, Google or Outlook Calendar; **Open in calendar app** does that on the device you're on. Each warranty you can see appears on the day it ends, with when and where it was bought and a link back to it. Lifetime, void and archived warranties and drafts are left out. Calendar apps check the link a few times a day, so changes show up there with a delay.

Anyone with the link can see your warranty end dates. **Get a new link** replaces it, and the old one stops working; **Turn off** removes it.

## Void, archiving and deleting

**Split off pieces** is for a warranty that covers several pieces whose paths part, for example when you sell one and lend another. It moves the pieces you choose, with their serial numbers, to a new warranty with the same purchase and coverage details, tags and documents, so each can be archived, voided or claimed on by itself. Claims stay with the original. On a warranty linked to HomeBox it can split the item too: the new warranty gets its own item in the same place, and the original item's quantity goes down.

**Mark as void** is for a warranty that no longer applies though it hasn't expired, for example after a drop, a repair somewhere else or a broken seal, while you still have the product. You can say why. A void warranty sends no reminders and doesn't count towards the covered value; it's under the **Void** filter, with the reason at the top of its page, and **Make valid again** undoes it.

**Archive** hides a warranty you no longer need, such as one for something you sold, without deleting it. It no longer sends reminders. Archived warranties are under the **Archived** filter, and **Restore from archive** brings one back.

**Delete** removes the warranty and the files stored in Warranteed. Documents in Paperless and items in HomeBox are kept.

## Tags

Tags are created by typing them into a warranty's tag field. Under **Settings → Tags** you can rename them, change their colour or delete them. Deleting a tag removes it from its warranties; the warranties are kept.

## Import and export

Under **Settings → Import & export**:

- **Export CSV** downloads all your warranties.
- **Import CSV** reads a file in the same format, for example from a spreadsheet. Rows that already exist are skipped, and rows with problems are listed with the reason.

The columns are `product_name`, `brand`, `model`, `vendor`, `order_number`, `purchase_date`, `warranty_months`, `warranty_end_date`, `lifetime`, `price`, `currency`, `quantity`, `serial_numbers`, `tags`, `product_url`, `notes`, `archived`, `void_reason` and `space`. Only `product_name` and `purchase_date` are required, plus one of `warranty_months`, `warranty_end_date` or `lifetime=true`. `price` is per piece, and an empty `quantity` means one. `void_reason` is why a warranty is void, or `void` for one without a reason, and empty otherwise. Separate several serial numbers or tags with `;`. Dates are `YYYY-MM-DD`.

Documents aren't part of the CSV.

## Settings

- **Display**: language (also used for reminders), date format, default currency, what "expiring soon" means, and the theme. You can also change the theme from the account menu.
- **Reminders**: see [Reminders](#reminders).
- **Paperless-ngx**: your API token (Paperless: *Profile → API Auth Token*), whether uploads go to Paperless by default, links back from Paperless, and drafts.
- **HomeBox**: your API key, and whether product photos are stored in HomeBox by default.
- **Household**: who you share with, and sharing everything at once.
- **Tags**, and **Import & export**.

Changes save automatically.
