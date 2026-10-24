# Warranteed

Self-hosted warranty tracker. Track purchases, warranty periods, receipts and manuals, and get reminded before a warranty expires.

## Features

- Sign-in through an OpenID Connect provider (Pocket ID, Authentik, Keycloak, …). There are no local passwords.
- Store receipts and manuals locally or in Paperless-ngx, or link documents already in Paperless.
- Expiry reminders via ntfy, Gotify, Discord, Telegram, email and other services.
- Tags, serial numbers, archiving, search, CSV export and import.
- Light and dark theme, usable on phones.

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

- `OIDC_ALLOWED_GROUPS`: only members of these groups can sign in.
- `OIDC_ADMIN_GROUPS`: members of these groups are admins. If unset, the first user to sign in is the admin.

## Paperless-ngx

Set `PAPERLESS_URL` and `PAPERLESS_TOKEN` in `.env`, or per user under **Settings → Paperless-ngx**. The token is under *Profile → API Auth Token* in Paperless. If `PAPERLESS_URL` is an internal address, set `PAPERLESS_PUBLIC_URL` to the address your browser uses.

Removing a Paperless attachment only unlinks it; the document stays in Paperless.

## Reminders

Under **Settings → Reminders**, add one or more [Shoutrrr URLs](https://shoutrrr.nickfedor.com/latest/services/overview/) and the number of days before expiry to send a reminder (e.g. `30, 7`). Reminders are checked once a day after `WARRANTEED_NOTIFY_HOUR` in the `TZ` timezone. Each reminder is sent once.

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
| `OIDC_SCOPES` | `openid profile email groups` | |
| `OIDC_ALLOWED_GROUPS` | | Comma-separated groups allowed to sign in |
| `OIDC_ADMIN_GROUPS` | | Comma-separated admin groups |
| `OIDC_GROUPS_CLAIM` | `groups` | Claim containing the user's groups |
| `OIDC_AUTO_REDIRECT` | `false` | Skip the login page |
| `OIDC_LOGOUT_AT_PROVIDER` | `false` | Also sign out at the provider |
| `PAPERLESS_URL` | | Paperless-ngx URL |
| `PAPERLESS_TOKEN` | | Paperless-ngx API token |
| `PAPERLESS_PUBLIC_URL` | `PAPERLESS_URL` | Paperless URL used for browser links |
| `TZ` | | Timezone |
| `WARRANTEED_NOTIFY_HOUR` | `9` | Hour (0–23) after which reminders are sent |
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

Back up both directories. To copy the database while the app is running, use `sqlite3 warranteed.db ".backup backup.db"`.

The container runs as uid 65532. Bind mounts and network shares must be writable by that user, or set `user:` in the compose file.

## Development

Requires Go 1.27+ and Node 24+.

```sh
make dev    # backend on :8080 with a development login (do not use in production)
make web    # frontend dev server on :5173
make test
```

`dev/compose.yml` starts Paperless-ngx and a mock OIDC provider for local testing.

## License

MIT
