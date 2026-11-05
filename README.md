# Warranteed

[![CI](https://img.shields.io/github/actions/workflow/status/fhavrlent/warranteed/ci.yml?branch=main&label=ci&logo=github)](https://github.com/fhavrlent/warranteed/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/fhavrlent/warranteed?sort=semver&label=release)](https://github.com/fhavrlent/warranteed/pkgs/container/warranteed)
[![License](https://img.shields.io/github/license/fhavrlent/warranteed)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/fhavrlent/warranteed?logo=go&logoColor=white)](go.mod)

Self-hosted warranty tracker. Track purchases, warranty periods, receipts and manuals, and get reminded before a warranty expires.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/list-dark.png">
  <img alt="Warranty list with expiry status, tags and value under warranty" src="docs/screenshots/list.png">
</picture>

<table>
  <tr>
    <td width="40%"><img alt="Warranty detail with receipt and warranty card" src="docs/screenshots/detail.png"></td>
    <td width="40%"><img alt="Adding a warranty" src="docs/screenshots/add.png"></td>
    <td width="20%"><img alt="Warranty list on a phone" src="docs/screenshots/phone.png"></td>
  </tr>
</table>

## Features

- Sign-in through an OpenID Connect provider (Pocket ID, Authentik, Keycloak, …). There are no local passwords.
- Store receipts and manuals locally or in Paperless-ngx, or link documents already in Paperless.
- Add warranties to your [Wherehouse](https://github.com/fhavrlent/wherehouse) or HomeBox inventory, or link them to things already there.
- Expiry reminders via ntfy, Gotify, Discord, Telegram, email and other services.
- A private calendar feed of warranty end dates for Apple, Google or Outlook Calendar.
- Repairs and claims with their documents, copied to the HomeBox item's maintenance log.
- Tags, serial numbers, quantities, void warranties, archiving, search, CSV export and import.
- Light and dark theme, usable on phones.
- In English and Czech; more languages are easy to add.

The [user guide](docs/user-guide.md) shows how to use it, and the [integration guide](docs/integrations.md) sets up Paperless-ngx, paperless-gpt, Claude, HomeBox and reminders step by step.

## Tested with

| | Version |
| --- | --- |
| Paperless-ngx | 3.3.4 (API v10). Documents also work with 2.20.15 (API v9) |
| HomeBox | 0.27.1; earlier versions lack the API Warranteed uses |
| Claude | `claude-haiku-5-5`, the default model for reading receipts |

Drafts from Paperless custom fields were tested with values typed into Paperless by hand, not with paperless-gpt itself. Newer versions of each will probably work; please [open an issue](https://github.com/fhavrlent/warranteed/issues) if one doesn't.

## Installation

```sh
curl -O https://raw.githubusercontent.com/fhavrlent/warranteed/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/fhavrlent/warranteed/main/.env.example
```

Edit `.env` (at least `WARRANTEED_APP_URL` and the `OIDC_*` values), then:

```sh
docker compose up -d
```

Run it behind a reverse proxy with HTTPS and set `WARRANTEED_APP_URL` to the public URL. If the proxy asks people to sign in (Authelia, an Authentik outpost and the like), let `/calendar/` through without it: calendar apps can't sign in, and those links carry their own secret.

## Sign-in

Create an OIDC client in your identity provider with the callback URL `https://<your host>/auth/callback`, then set:

```env
OIDC_ISSUER=https://id.example.com
OIDC_CLIENT_ID=...
OIDC_CLIENT_SECRET=...
OIDC_PROVIDER_NAME=Pocket ID
```

`OIDC_ISSUER` is the URL that serves `/.well-known/openid-configuration`. The client secret can be left empty for public clients.

- `OIDC_ALLOWED_GROUPS`: only members of these groups can sign in. Not needed if your provider already restricts the client to certain groups (Pocket ID does).
- `OIDC_ADMIN_GROUPS`: members of these groups are admins. If unset, the first user to sign in is the admin.

Group membership is checked at sign-in and again every hour (`OIDC_RECHECK_INTERVAL`) using a refresh token, so someone removed from the group, or deleted in the provider, is signed out within an hour. This needs the `offline_access` scope, which is requested by default. If your provider issues no refresh token, sessions last at most 7 days instead.

## Sharing

Everyone who signs in belongs to one household. New warranties are shared with the household by default: everyone can see and edit them, add documents and archive them. Choose **Only me** to keep one private. In the household, only the person who added a warranty can delete it or make it private again.

Documents a person attached from their Paperless are shown to the household with that person's connection, so others can open those documents but nothing else in their Paperless. Reminders go to each person through their own notification settings.

With a single user, none of this appears in the interface.

## Paperless-ngx

Set `PAPERLESS_URL` to your Paperless instance. Each user then connects with their own API token under **Settings → Paperless-ngx** (in Paperless: *Profile → API Auth Token*), so Paperless's permissions decide which documents they can see. Giving each person their own Paperless user is recommended. If `PAPERLESS_URL` is an internal address, set `PAPERLESS_PUBLIC_URL` to the address your browser uses. An admin who sets their own Paperless URL in Settings can set its link address there too.

`PAPERLESS_TOKEN` is optional and only ever used for admins; other users never get it. Only admins can point Paperless at a different URL.

Removing a Paperless attachment only unlinks it; the document stays in Paperless.

Under **Settings → Paperless-ngx**, each user can also have their documents link back: Warranteed fills a "Warranty" URL custom field on each of their documents attached to a warranty, and a "HomeBox" one when the warranty is linked to a HomeBox item, so a receipt in Paperless leads to its warranty and the item. The fields are created on first use; rename them with `PAPERLESS_WARRANTY_FIELD` and `PAPERLESS_HOMEBOX_FIELD` (e.g. `Záruka`). Their Paperless token must be allowed to edit documents, and to create custom fields if those don't exist yet. A value someone typed into these fields by hand is never overwritten.

### Drafts from new documents

Warranteed can turn receipts added to Paperless into draft warranties, so you only check and confirm them. It's off until an admin enables it and each user opts in.

1. Set `WARRANTEED_WEBHOOK_SECRET` to a long random string (e.g. `openssl rand -hex 32`).
2. In Paperless, add a workflow with the trigger *Document added*, filtered the way you like (a tag such as `warranty` keeps out other paperwork), and a *Webhook* action:
   - URL: `http://<warranteed>:8080/hooks/paperless` (Warranteed's internal address is fine, as Paperless allows internal addresses by default)
   - Header: `X-Warranteed-Secret` set to the secret
   - Body: `{"doc_id": "{{doc_id}}"}`
3. Each user picks under **Settings → Paperless-ngx → Drafts from new documents** whether they get drafts and where their details come from:
   - **the document's own details:** title, date and correspondent;
   - **Paperless custom fields**, for example ones [paperless-gpt](https://github.com/icereed/paperless-gpt) fills in. Map them with `PAPERLESS_DRAFT_FIELDS`, e.g. `product=Product, price=Price, warranty_months=Warranty months` (also `brand`, `model`, `vendor`, `purchase_date`, `currency`, `quantity`, `serial`, `order`). If the fields are still empty when the document arrives, the draft is filled in once they have values, unless it was edited already;
   - **Claude**, which reads the document itself, when `ANTHROPIC_API_KEY` is set. The document is sent to Anthropic. A receipt for several devices gives a draft for each, leaving out delivery, food and other things nobody claims a warranty on, or, if you choose, a draft for every product (up to 30).

A draft goes to whoever owns the document in Paperless, matched through their Paperless token; a document nobody here owns goes to an admin. Only people who opted in get drafts. Drafts are listed under **To review** only for the person they were made for, count toward no totals and send no reminders until confirmed.

## HomeBox

With [HomeBox](https://homebox.software) (v0.27 or later), a warranty can be added to your inventory or linked to an item that's already there. Set `HOMEBOX_URL` to your HomeBox instance, and `HOMEBOX_PUBLIC_URL` if that's an internal address. Each user adds their own API key under **Settings → HomeBox** (in HomeBox: *Profile → API keys*). Keys expire after 30 days unless you give them a later date.

On a warranty, **Add to HomeBox** creates an item with the serial number, purchase date, price, seller and warranty details, plus links back to the warranty and to its receipts in Paperless. **Link existing item** searches HomeBox instead and only fills in fields that are empty. Product photos are copied to the item, including ones added later, or can be stored only in HomeBox and shown from there. The warranty then shows the item's asset ID, the number on HomeBox labels, and searching for it in Warranteed finds the warranty.

## Wherehouse

With [Wherehouse](https://github.com/fhavrlent/wherehouse), warranted products also live in your inventory. Set `WHEREHOUSE_URL` (and `WHEREHOUSE_PUBLIC_URL` if browsers use another address); each person then clicks **Connect to Wherehouse** under **Settings → Wherehouse** and allows it there. There's no key to copy.

On a warranty, **Add to Wherehouse** makes a thing in a place you pick, or nowhere yet; **Link existing thing** finds one. Warranteed then owns the thing's purchase and warranty details (Wherehouse shows them read-only and keeps them up to date), copies the warranty's photos and receipts to the thing, including ones added later, and keeps its claims in the thing's maintenance log. Wherehouse owns where it is, its photos, label, insurance and sale: the warranty shows where the thing is and its label code, which you can search for here, and selling or archiving the thing in Wherehouse archives the warranty within seconds. Confirming a draft can add its product to Wherehouse straight away. With **Keep product photos only in Wherehouse** on, a linked warranty's photos are kept only on the thing and shown from there, so there's one copy; they come back to Warranteed if the warranty is unlinked.

## Reminders

Under **Settings → Reminders**, add one or more [Shoutrrr URLs](https://shoutrrr.nickfedor.com/latest/services/overview/) and the number of days before expiry to send a reminder (e.g. `30, 7`). Reminders are checked once a day after `WARRANTEED_NOTIFY_HOUR` in the `TZ` timezone. Each reminder is sent once.

Discord, Telegram, Slack, Pushover, Pushbullet, IFTTT and Join work for everyone. Services where the URL contains a server address (ntfy, Gotify, generic webhooks, email and others) are unrestricted for admins; other users can only use them with hosts listed in `WARRANTEED_NOTIFY_ALLOWED_HOSTS`, so the server can't be used to reach internal addresses.

```
ntfy://ntfy.sh/my-warranties
smtp://user:password@smtp.example.com:587/?from=warranty@example.com&to=me@example.com
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `WARRANTEED_APP_URL` | `http://localhost:8080` | Public URL |
| `OIDC_ISSUER` | | Identity provider issuer URL (required) |
| `OIDC_CLIENT_ID` | | Client ID (required) |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_PROVIDER_NAME` | `SSO` | Login button label |
| `OIDC_SCOPES` | `openid profile email groups offline_access` | |
| `OIDC_ALLOWED_GROUPS` | | Comma-separated groups allowed to sign in |
| `OIDC_ADMIN_GROUPS` | | Comma-separated admin groups |
| `OIDC_GROUPS_CLAIM` | `groups` | Claim containing the user's groups |
| `OIDC_AUTO_REDIRECT` | `false` | Skip the login page |
| `OIDC_LOGOUT_AT_PROVIDER` | `false` | Also sign out at the provider |
| `OIDC_RECHECK_INTERVAL` | `1h` | How often group membership is re-checked |
| `PAPERLESS_URL` | | Paperless-ngx URL |
| `PAPERLESS_TOKEN` | | API token used for admins only |
| `PAPERLESS_PUBLIC_URL` | `PAPERLESS_URL` | Paperless URL used for browser links |
| `PAPERLESS_WARRANTY_FIELD` | `Warranty` | Paperless custom field that links a document to its warranty |
| `PAPERLESS_HOMEBOX_FIELD` | `HomeBox` | Paperless custom field that links a document to its HomeBox item |
| `WARRANTEED_WEBHOOK_SECRET` | | Turns on drafts from new Paperless documents; the webhook must send it in `X-Warranteed-Secret` |
| `PAPERLESS_DRAFT_FIELDS` | | Paperless custom fields drafts can read details from, e.g. `product=Product, price=Price` |
| `ANTHROPIC_API_KEY` | | Lets users have Claude read new documents for drafts |
| `WARRANTEED_CLAUDE_MODEL` | `claude-haiku-5-5` | Claude model that reads documents |
| `HOMEBOX_URL` | | HomeBox URL |
| `HOMEBOX_PUBLIC_URL` | `HOMEBOX_URL` | HomeBox URL used for browser links |
| `WHEREHOUSE_URL` | | Wherehouse URL |
| `WHEREHOUSE_PUBLIC_URL` | `WHEREHOUSE_URL` | Wherehouse URL used for browser links |
| `TZ` | | Timezone |
| `WARRANTEED_NOTIFY_HOUR` | `9` | Hour (0–23) after which reminders are sent |
| `WARRANTEED_NOTIFY_ALLOWED_HOSTS` | | Comma-separated hosts non-admins may use with self-hosted notification services |
| `WARRANTEED_SECRET` | | Key for encrypting stored refresh tokens; generated in the data directory if unset |
| `WARRANTEED_MAX_UPLOAD_MB` | `50` | Maximum upload size |
| `WARRANTEED_SESSION_DAYS` | `30` | Session length |
| `WARRANTEED_DATA_DIR` | `/data` | Database directory (local disk) |
| `WARRANTEED_FILES_DIR` | `/data/files` | Uploaded files directory |
| `WARRANTEED_LISTEN` | `:8080` | Listen address |

## Storage and backups

The database (`warranteed.db`, SQLite) is in `WARRANTEED_DATA_DIR`. Keep it on local disk; SQLite does not work reliably on network file systems such as NFS or SMB.

Uploaded files are in `WARRANTEED_FILES_DIR` and can be on network storage:

```yaml
services:
  warranteed:
    environment:
      WARRANTEED_FILES_DIR: /files
    volumes:
      - warranteed-data:/data
      - /mnt/nas/warranteed:/files
```

Small previews of product photos are kept there too, as `*.thumb.jpg`; they can be left out of backups, as they're made again when needed. Back up both directories. The data directory also holds `secret.key` (unless `WARRANTEED_SECRET` is set); losing it only means everyone signs in again. To copy the database while the app is running, use `sqlite3 warranteed.db ".backup backup.db"`.

The container runs as uid 65532. Bind mounts and network shares must be writable by that user, or set `user:` in the compose file.

## Development

Requires Go 1.27+ and Node 24+.

```sh
make dev    # backend on :8080 with a development login (do not use in production)
make web    # frontend dev server on :5173
make test
```

`dev/compose.yml` starts Paperless-ngx and a mock OIDC provider for local testing.

## Translations

The app and its reminders are available in English and Czech. Each user can pick a language under **Settings → Display**; by default it follows the browser. To add or improve a language, see [TRANSLATING.md](TRANSLATING.md). It only takes one JSON file.

## License

MIT
