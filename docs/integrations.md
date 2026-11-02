# Integration guide

Step-by-step setup of what Warranteed connects to: Paperless-ngx for documents, paperless-gpt or Claude for drafts, HomeBox for your inventory, and notification services for reminders. Every option is listed in the [README's configuration table](../README.md#configuration), and the versions Warranteed was tested with under [Tested with](../README.md#tested-with). How it all looks to users is in the [user guide](user-guide.md).

- [How the pieces fit](#how-the-pieces-fit)
- [Paperless-ngx](#paperless-ngx)
  - [Connecting](#connecting)
  - [Links back from Paperless](#links-back-from-paperless)
  - [Drafts from new documents](#drafts-from-new-documents)
  - [Draft details from paperless-gpt](#draft-details-from-paperless-gpt)
  - [Draft details from Claude](#draft-details-from-claude)
- [HomeBox](#homebox)
- [Reminders](#reminders)
- [Troubleshooting](#troubleshooting)

## How the pieces fit

Receipts go into Paperless, Warranteed drafts or tracks the warranties, and HomeBox holds the inventory, with links between all three. Each part is optional and works on its own:

| You want | Set up |
| --- | --- |
| Receipts kept in Paperless, not in Warranteed | [Connecting](#connecting) |
| Paperless documents that link to their warranty | [Links back](#links-back-from-paperless) |
| Warranties drafted from receipts you scan | [Drafts](#drafts-from-new-documents), plus [paperless-gpt](#draft-details-from-paperless-gpt) or [Claude](#draft-details-from-claude) for better details |
| Warranties in your HomeBox inventory | [HomeBox](#homebox) |
| A message before a warranty expires | [Reminders](#reminders) |

Paperless and HomeBox are set up in two halves. The admin points the server at them with environment variables. Each user then connects with their own token or key in **Settings**, so Paperless and HomeBox permissions decide what each person can see.

## Paperless-ngx

### Connecting

1. Set `PAPERLESS_URL` to where Warranteed reaches Paperless. In Docker this is often an internal address such as `http://paperless:8000`; then set `PAPERLESS_PUBLIC_URL` to the address browsers use, e.g. `https://paperless.example.com`, so links open.
2. Restart Warranteed.
3. Each user creates an API token in Paperless (*Profile → API Auth Token*), pastes it under **Settings → Paperless-ngx** and clicks **Test connection**.

Give each person their own Paperless user. Their token only sees what their Paperless user may see, and documents they upload belong to them, which is also how drafts find their owner.

Admins can skip the token when `PAPERLESS_TOKEN` is set; the server's token is never used for anyone else. An admin can also point their own connection at a different Paperless, and give it its own link address.

What the token needs:

| Feature | Paperless permissions |
| --- | --- |
| Linking and viewing documents | View documents |
| Storing uploads in Paperless | Add documents |
| Links back from Paperless | Change documents; add custom fields, unless the fields already exist |
| Drafts | View documents |

### Links back from Paperless

Turn on **Link documents back to their warranty** under **Settings → Paperless-ngx**. Warranteed then fills two URL custom fields on each document attached to a warranty:

- **Warranty**: the warranty in Warranteed;
- **HomeBox**: the warranty's HomeBox item, when HomeBox is set up and the warranty is linked to one.

The fields are created the first time they're needed. For other names, e.g. in your language, set `PAPERLESS_WARRANTY_FIELD` and `PAPERLESS_HOMEBOX_FIELD` before turning links on. If a field of that name already exists, it must be a URL field.

The links follow changes: attaching, unlinking, deleting a warranty and linking HomeBox all update them. When a document is attached to several warranties, as with drafts from a receipt for several devices, the Warranty link opens the list of all of them, and the HomeBox link opens HomeBox's list of their items. For that, Warranteed gives each of those items a "Warranteed" text field holding a short value for the receipt, such as `145-1a2b3c`, so the link stays short however many items there are; an item on several such receipts gets one field per receipt, and items unlinked from a receipt lose its value. If HomeBox can't be reached at the time, the link points to the first item until the next update. A value someone typed into these fields by hand is never overwritten. Turning links on also fills them in on documents that were attached before.

### Drafts from new documents

New documents in Paperless can become draft warranties for their owner to check and confirm. This uses a workflow with a webhook action, so Paperless must be recent enough to have them.

**1. Turn it on in Warranteed.** Set a secret, for example from `openssl rand -hex 32`, and restart:

```env
WARRANTEED_WEBHOOK_SECRET=<long random string>
```

**2. Add a Paperless workflow.** In Paperless, go to *Workflows → Add workflow*:

- **Trigger:** *Document added*. Add a filter so only receipts count, for example the tag `warranty`, or a mail rule or consume folder used for receipts. Without one, every new document becomes a draft.
- **Action:** *Webhook*:
  - **URL:** `http://warranteed:8080/hooks/paperless`, or wherever Paperless reaches Warranteed. An internal Docker address is fine.
  - **Headers:** `X-Warranteed-Secret` set to the secret
  - **Body:** `{"doc_id": "{{doc_id}}"}`, with *Send as JSON* on (off also works).

**3. Each user opts in** under **Settings → Paperless-ngx → Drafts from new documents** and picks where the details come from. Users who leave it off get no drafts.

How a draft is made:

- It goes to the person who owns the document in Paperless. Warranteed finds them by asking Paperless whose each saved token is. A document whose owner doesn't use Warranteed, or that has no owner, goes to the first admin who turned drafts on.
- A document already attached to a warranty makes no new draft, so a repeated webhook does no harm.
- The document is attached as the proof of purchase, and the draft starts with the document's title, date and correspondent, and a 24-month warranty. paperless-gpt or Claude then fill in what they find.

### Draft details from paperless-gpt

[paperless-gpt](https://github.com/icereed/paperless-gpt) can fill Paperless custom fields with an LLM. Warranteed reads those fields into the draft.

1. In Paperless, create the custom fields you want filled, for example:

   | Detail | Example field | Field type |
   | --- | --- | --- |
   | `product` | Product | Text |
   | `brand`, `model`, `vendor`, `order` | Brand, Model, Vendor, Order number | Text |
   | `purchase_date` | Purchase date | Date |
   | `price` | Price | Monetary, or a number; the price of one piece |
   | `quantity` | Quantity | Integer; how many pieces were bought |
   | `currency` | Currency | Text, e.g. `EUR`. A monetary field's own currency is used otherwise |
   | `warranty_months` | Warranty months | Integer |
   | `serial` | Serial numbers | Text; separate several with `,` or `;` |

2. Set up paperless-gpt to fill them in; see its documentation for custom fields.
3. Map them in Warranteed with the field names as they are in Paperless:

   ```env
   PAPERLESS_DRAFT_FIELDS=product=Product, brand=Brand, model=Model, price=Price, warranty_months=Warranty months, purchase_date=Purchase date
   ```

4. Users pick **On, with Paperless custom fields (e.g. paperless-gpt)**.

paperless-gpt often runs after the document is added. If the fields are still empty when the draft is made, Warranteed looks again after 1, 3 and 10 minutes and fills the draft in, unless its owner has already edited it. Fields describe one product, so each document makes one draft.

### Draft details from Claude

Claude can read the receipt itself, including photos of till slips and documents in any language.

1. Create an API key in the Claude Console and set it:

   ```env
   ANTHROPIC_API_KEY=sk-ant-...
   ```

2. Optionally choose a different model with `WARRANTEED_CLAUDE_MODEL`. The default, `claude-haiku-5-5`, is the cheapest and read receipts well in testing.
3. Users pick **On, read by Claude**.

Claude finds the product name, brand, model, shop, purchase date, price of one piece including tax, how many pieces were bought, currency, order number, serial numbers, and the warranty length when the receipt states one. A receipt for several devices makes a draft for each, while delivery, discounts, food, books, music and similar items are left out; users who pick **Make drafts of: Every product** get a draft for every product instead. Either way, up to 30 per document. Names stay in the receipt's language. When the document also gives the date the goods were received, the draft starts on the date the user picked under **Count warranties from**, and offers the other one. An extended warranty bought with a product lengthens that product's warranty by its months and is mentioned in the notes.

What to know:

- **Privacy:** each document is sent to Anthropic. The setting says so to users who pick it.
- **Cost:** the API key's account pays for each document read. Only users who choose Claude cause reads, and only for documents the workflow sends.
- **Failures:** if Claude can't be reached or declines, the draft is made from the document's own details instead.
- **Size:** documents over 24 MB aren't sent.

## HomeBox

You need [HomeBox](https://homebox.software) v0.27 or later.

1. Set `HOMEBOX_URL` to where Warranteed reaches HomeBox, and `HOMEBOX_PUBLIC_URL` if browsers use another address. Restart.
2. Each user creates an API key in HomeBox (*Profile → API keys*) and saves it under **Settings → HomeBox**. Keys expire after 30 days unless given a later date, so a longer expiry saves renewing them.

On a warranty, **Add to HomeBox** creates an item: name, brand, model, serial number, purchase date, price, quantity, seller, warranty end date and details, plus links to the warranty and to its receipts in Paperless (with `PAPERLESS_PUBLIC_URL` as their address). It can go into a location or into an item that already holds other items. **Link existing item** finds items and locations, and fills in only the empty fields instead. Either way, the warranty's repairs and claims go into the item's maintenance log and stay in step with Warranteed.

Product photos are uploaded to the item too, then and whenever one is added to the warranty later, using the HomeBox key of whoever adds it. The first becomes the item's main photo unless it has one. Warranteed remembers which photos it sent, so none is sent twice.

On a linked warranty, users can also store new photos only in HomeBox: the upload goes to the item and Warranteed shows it from there, through the key of whoever added it. **Store product photos in HomeBox by default** under **Settings → HomeBox** makes this each user's default.

With links back from Paperless on, the receipts in Paperless then also link to the HomeBox item.

## Reminders

Reminders use [Shoutrrr URLs](https://shoutrrr.nickfedor.com/latest/services/overview/). Each user adds theirs under **Settings → Reminders**, with the days before expiry to be reminded, and can **Send test**.

```
ntfy://ntfy.sh/my-warranties
ntfy://ntfy.example.com/warranties
gotify://gotify.example.com/<app token>
discord://<token>@<webhook id>
telegram://<bot token>@telegram?chats=<chat id>
smtp://user:password@smtp.example.com:587/?from=warranty@example.com&to=me@example.com
```

Reminders are checked daily after `WARRANTEED_NOTIFY_HOUR` (default 9) in the `TZ` timezone, and each one is sent once.

Discord, Telegram, Slack, Pushover, Pushbullet, IFTTT and Join work for everyone. Services whose URL holds a server address, such as ntfy, Gotify, generic webhooks and email, are unrestricted for admins only. Other users can only use the hosts you list, so the server can't be made to reach internal addresses:

```env
WARRANTEED_NOTIFY_ALLOWED_HOSTS=ntfy.example.com, smtp.example.com
```

## Troubleshooting

**No drafts appear**
- Check the workflow's runs in Paperless. A 401 means the secret in the header doesn't match `WARRANTEED_WEBHOOK_SECRET`; a 404 means the secret or `PAPERLESS_URL` isn't set.
- The document's owner must have drafts on and be connected to the same Paperless as `PAPERLESS_URL`.
- Documents already attached to a warranty are skipped.
- Warranteed logs a line when a draft is made, and when the owner doesn't get drafts.

**Drafts have few details**
- With custom fields: check that the names in `PAPERLESS_DRAFT_FIELDS` match Paperless exactly, apart from case, and that paperless-gpt fills them within 10 minutes.
- With Claude: look for `read receipt with claude` in Warranteed's log.

**Links back don't appear in Paperless**
- The user's token must be allowed to change documents, and to add custom fields if they don't exist yet. Settings shows an error saying so.
- A field with the same name that isn't a URL field blocks it. Rename that field, or pick other names with `PAPERLESS_WARRANTY_FIELD` and `PAPERLESS_HOMEBOX_FIELD`.

**HomeBox stopped working**
- The API key has probably expired. Create a new one and save it in Settings.

**Links open an internal address**
- Set `PAPERLESS_PUBLIC_URL` or `HOMEBOX_PUBLIC_URL`, or, for an admin's own Paperless connection, its link address in Settings.
