# User guide

How to use Warranteed day to day. Setting up the server and its connections to Paperless-ngx, HomeBox and notification services is covered in the [README](../README.md) and the [integration guide](integrations.md).

- [Signing in](#signing-in)
- [Adding a warranty](#adding-a-warranty)
- [Documents and photos](#documents-and-photos)
- [Drafts from Paperless](#drafts-from-paperless)
- [Finding warranties](#finding-warranties)
- [Sharing with the household](#sharing-with-the-household)
- [HomeBox](#homebox)
- [Reminders](#reminders)
- [Archiving and deleting](#archiving-and-deleting)
- [Tags](#tags)
- [Import and export](#import-and-export)
- [Settings](#settings)

## Signing in

Click **Continue with …** and sign in with your identity provider. Warranteed has no passwords of its own. Your name, email and picture come from the provider.

## Adding a warranty

Click **Add warranty** and fill in:

- **Product**, and optionally the brand and model.
- **Purchase date**, where you bought it, the price and the order number.
- **Warranty**, in one of three ways:
  - **Length** in years or months, from the purchase date;
  - **End date**, when the warranty card gives a date;
  - **Lifetime**, for warranties that never expire.
- **Serial numbers**. Add as many as you need, for example for a set of several devices.
- **Tags**, a product link and notes, such as the support phone number or how to make a claim.
- **Who can see this**: the household, or only you. See [Sharing](#sharing-with-the-household).

You can drop the receipt and other documents into the same form. They're uploaded when you save.

## Documents and photos

Each warranty can hold its receipt, warranty terms and card, extended warranty, repair and claim reports, manuals, product photos and other files. When adding files, you choose a type for each one, so the right file is easy to find when you make a claim.

With Paperless-ngx connected:

- Documents can be **stored in Paperless** instead of in Warranteed. The upload dialog asks where to store them. **Upload documents to Paperless by default** in Settings makes Paperless the default. Product photos are always stored in Warranteed, and so are file types Paperless can't take.
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
| **On, read by Claude** | Claude reads the receipt itself. The document is sent to Anthropic | One per device on it, up to 10 |

Which documents become drafts is decided by the Paperless workflow your admin set up, often by a tag such as `warranty`. Only documents you own in Paperless become your drafts.

When a draft arrives, the list shows a banner and the **To review** filter. On each draft:

1. Check the details and correct them with **Edit**. Anything the receipt doesn't say is left for you; a warranty length that isn't stated defaults to 24 months.
2. Click **Confirm** to make it an ordinary warranty, or delete it if you don't want to track it.

Until it's confirmed, a draft is visible only to you, sends no reminders and isn't counted in totals.

With Claude, a receipt for several devices gives a draft for each, with the shop, date, order number and receipt filled in on all of them. Delivery, discounts, food and other things nobody claims a warranty on are left out. Delete any draft you don't need.

With custom fields, the draft may be made before paperless-gpt has filled them in. Warranteed then looks again over the next ten minutes and fills the draft in, unless you've already edited it.

## Finding warranties

- **Search** looks through products, brands, models, stores, order numbers, serial numbers, tags, notes and HomeBox asset IDs. It ignores accents and allows small typos.
- **Filters**: Current, Expired, Archived, All, and To review when you have drafts. With a household, you can also show only the shared warranties or only your private ones.
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

## HomeBox

With HomeBox connected (**Settings → HomeBox**, with an API key from HomeBox's *Profile → API keys*), a warranty can be part of your inventory:

- **Add to HomeBox** creates an item with the serial number, purchase date, price, seller and warranty details, a link to the warranty, and links to its receipts in Paperless. You can pick a location.
- **Link existing item** searches HomeBox by name, serial number or #asset ID. Only empty fields on the item are filled in; nothing already there changes.
- The warranty then shows the item's asset ID, the number printed on HomeBox labels, and a link to open it in HomeBox.
- **Unlink** makes the warranty forget the item. The item stays in HomeBox.

HomeBox keys expire after 30 days unless you give them a later date. When yours expires, create a new one and save it in Settings.

## Reminders

Under **Settings → Reminders**, add one or more notification URLs and when to be reminded, for example `30, 7` for 30 and 7 days before a warranty expires. **Send test** checks that the URLs work. Each reminder is sent once, in your language.

Discord, Telegram, Slack, Pushover, Pushbullet, IFTTT and Join work for everyone. Self-hosted services such as ntfy, Gotify, webhooks and email need an admin to allow their server first. The [integration guide](integrations.md#reminders) has URL examples.

## Archiving and deleting

**Archive** hides a warranty you no longer need, such as one for something you sold, without deleting it. It no longer sends reminders. Archived warranties are under the **Archived** filter, and **Restore from archive** brings one back.

**Delete** removes the warranty and the files stored in Warranteed. Documents in Paperless and items in HomeBox are kept.

## Tags

Tags are created by typing them into a warranty's tag field. Under **Settings → Tags** you can rename them, change their colour or delete them. Deleting a tag removes it from its warranties; the warranties are kept.

## Import and export

Under **Settings → Import & export**:

- **Export CSV** downloads all your warranties.
- **Import CSV** reads a file in the same format, for example from a spreadsheet. Rows that already exist are skipped, and rows with problems are listed with the reason.

The columns are `product_name`, `brand`, `model`, `vendor`, `order_number`, `purchase_date`, `warranty_months`, `warranty_end_date`, `lifetime`, `price`, `currency`, `serial_numbers`, `tags`, `product_url`, `notes`, `archived` and `space`. Only `product_name` and `purchase_date` are required, plus one of `warranty_months`, `warranty_end_date` or `lifetime=true`. Separate several serial numbers or tags with `;`. Dates are `YYYY-MM-DD`.

Documents aren't part of the CSV.

## Settings

- **Display**: language (also used for reminders), date format, default currency, what "expiring soon" means, and the theme. You can also change the theme from the account menu.
- **Reminders**: see [Reminders](#reminders).
- **Paperless-ngx**: your API token (Paperless: *Profile → API Auth Token*), whether uploads go to Paperless by default, links back from Paperless, and drafts.
- **HomeBox**: your API key.
- **Household**: who you share with, and sharing everything at once.
- **Tags**, and **Import & export**.

Changes save automatically.
