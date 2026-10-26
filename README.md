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
- Expiry reminders via ntfy, Gotify, Discord, Telegram, email and other services.
- Tags, serial numbers, archiving, search, CSV export and import.
- Light and dark theme, usable on phones.
- In English and Czech; more languages are easy to add.

## Installation

```sh
curl -O https://raw.githubusercontent.com/fhavrlent/warranteed/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/fhavrlent/warranteed/main/.env.example
```

Edit `.env` (at least `WARRANTEED_APP_URL` and the `OIDC_*` values), then:

```sh
docker compose up -d
```

Run it behind a reverse proxy with HTTPS and set `WARRANTEED_APP_URL` to the public URL.

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

Back up both directories. The data directory also holds `secret.key` (unless `WARRANTEED_SECRET` is set); losing it only means everyone signs in again. To copy the database while the app is running, use `sqlite3 warranteed.db ".backup backup.db"`.

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
