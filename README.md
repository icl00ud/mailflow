# mailflow

Post-purchase email journey for our Shopify stores (Arman Curren first). Go, Postgres, no framework.
Shopify webhooks -> scheduled jobs in Postgres -> SMTP, with a React panel that maps the journey, its
conditions, per-step delivery counts and every order's timeline.

## Journey (stores/arman-curren/flow.yaml)

| Step | Trigger | Wait | Rule |
|---|---|---|---|
| 1 Order confirmed | `orders/paid` | none | ignores the send window |
| 2 Being prepared | after the sale | 24 h exact | all orders |
| 2.5 Packed, waiting for carrier | after step 2 | 24 business h | all orders |
| 3 Shipped + tracking | after step 2.5 | 24 business h | **held until a real tracking number exists** |
| 4 No movement yet, normal | after step 3 | 90 business h | only if carrier silent, within 8 days of dispatch |
| 5 Waiting for the flight | after step 4 | 24 business h | skipped if already arrived |
| 6 Arrived in your country | carrier event `arrived_country` | none | |
| 7 Out for delivery today | carrier event `out_for_delivery` | none | ignores the send window |
| 8 Waiting for pickup | carrier event `ready_for_pickup` | none | **draft: copy not written, never sends** |

Business hours = clock hours counted only Mon-Fri (America/New_York, `holidays:` list skipped). Emails due outside
08:00-21:00 ET wait for the next 08:00. A skipped or paused step still releases the steps chained after it.

## Delivery contract

At-most-once. A job is claimed (`sending`) before the SMTP call. If the failure happened *before* the DATA phase
(dial/auth/RCPT) it retries 3x with backoff; any ambiguous failure (write/commit) becomes `failed` and waits for a
human ("Reenviar" in the dashboard or on the order). Jobs found in `sending` after a restart are marked `failed`, never resent.
`UNIQUE(order_id, step_id)` plus idempotent webhooks mean a redelivered `orders/paid` cannot send twice.

## Run

```bash
go test ./...                                     # unit + contract tests
TEST_DATABASE_URL=postgres://... go test ./...    # also runs the Postgres contract
go run ./cmd/mailflow -check                      # validate flows + templates
PUBLIC_ORIGIN=http://localhost:8080 COOKIE_SECURE=false \
  go run ./cmd/mailflow                           # in-memory store, dry-run sender; login needs an invited user
go run ./cmd/mailflow -demo                       # seeded demo + one-time invite URL (see below)
```

### Panel (React SPA)

The panel under `web/` is React 18 + TypeScript + Vite + Tailwind + shadcn/ui. Vite writes the
content-hashed bundle to `internal/server/web/app`, which is embedded into the Go binary. The
server-rendered login/invite pages share the same design tokens (`internal/server/web/static/auth.css`).

```bash
cd web && npm install          # once
cd web && npm run dev          # http://localhost:5173, proxies /api, /login, /invite to :8080
cd web && npm run build        # tsc + Vite -> internal/server/web/app (commit the result for go test CI)
```

The dev proxy rewrites `Origin` to `http://localhost:8080`, so the Go CSRF check keeps working.
The committed `app/` bundle lets `go test ./...` (and `go build`) run without Node; the Dockerfile
rebuilds it in a `node:22-alpine` stage.

### Demo mode

```bash
go run ./cmd/mailflow -demo
# prints: demo invite (one-time, 48h): http://localhost:8080/invite/<token>
```

`-demo` refuses to start when `DATABASE_URL` is set, forces the in-memory store, the log sender,
`PUBLIC_ORIGIN=http://localhost:8080` and `COOKIE_SECURE=false`, and seeds a tenant/user plus orders
in sent, scheduled, held, skipped, failed and canceled states. The one-time invite sets the password
for `demo@mailflow.local`; after that every page can be reviewed locally.

Env: `TRACK17_KEY` (17TRACK; unset = no automatic carrier events), `DATABASE_URL` (unset = memory),
`SMTP_HOST/PORT/USER/PASS` (unset = dry-run log sender), `SHOPIFY_WEBHOOK_SECRET_<STORE_ID>` (`ARMAN_CURREN`),
`STORES_DIR`, `LISTEN_ADDR`, and the panel/auth settings below.

### panel + auth env

| Var | Default | Meaning |
|---|---|---|
| `PUBLIC_ORIGIN` | `https://notify.armancurren.com` | Exact public origin of the panel. Every `POST/PUT/PATCH/DELETE` outside `/webhooks/*` must carry a matching `Origin` (or `Referer`) or it is rejected with 403. Also the WebAuthn origin. |
| `COOKIE_SECURE` | `true` | `Secure` flag on the `mf_session` cookie. Set `false` only for plain-HTTP local runs. |
| `WEBAUTHN_RP_ID` | host of `PUBLIC_ORIGIN` | Relying Party ID for passkeys. Must be the registrable domain (or host) the panel is served from; changing it invalidates existing passkeys. |
| `WEBAUTHN_RP_NAME` | `mailflow` | Name shown by the browser in the passkey prompt. |

`ADMIN_PASSWORD` and HTTP Basic auth are gone; `/healthz` stays public and the webhooks keep their own
HMAC/17TRACK-signature auth.

## Accounts, tenancy and onboarding

- **Tenants own stores.** `tenants` + `tenant_stores` map each `stores/<id>` flow to one tenant; `users` +
  `memberships` (role `owner` or `member`) decide who may see it. A session only ever lists, previews, tests,
  creates orders in, or acts on the stores of its own tenants — anything else is a 404, so the panel does not
  confirm that another tenant's store exists.
- **Sessions** are server-side: the `mf_session` cookie holds a random token, the `sessions` table holds its
  SHA-256, `HttpOnly`, `SameSite=Lax`, 30 days, `POST /logout` deletes the row.
- **Passkeys** live in `webauthn_credentials`; register/list/delete on `/account`, and `/login` offers
  usernameless ("discoverable") sign-in. Ceremony state is stored server-side, expires in 5 minutes and is
  deleted when used, so a challenge cannot be replayed.
- **No plaintext passwords are ever handed out.** Create the tenant and invite the person:

```bash
mailflow tenant add --id acme --name "Acme" --store acme-store
mailflow invite --tenant acme --email someone@example.com [--role owner]
# prints: https://notify.armancurren.com/invite/<one-time-token>
```

The invite is valid 48 hours and single use; the person sets a password (min 12 chars) and is then offered a
passkey. Running `mailflow` with no subcommand starts the server as before.

On first boot, if no tenant exists, the app seeds tenant `arman-curren` ("Arman Curren") owning store
`arman-curren`, so the existing deployment keeps working.

## Shopify setup

Set `store.shopify_domain` in flow.yaml, create a custom app, and subscribe webhooks to
`https://<host>/webhooks/shopify` for `orders/paid`, `orders/cancelled`, `fulfillments/create`, `fulfillments/update`.
Shopify maps `shipment_status` to in_transit / out_for_delivery / ready_for_pickup / delivered. It has **no** "arrived in
destination country" milestone, so when a tracking number is attached mailflow registers it with 17TRACK
(`tag = mf:<order id>`) and 17TRACK pushes milestones to `/webhooks/17track` (signed: `sha256(body + "/" + key)`).
Set that URL in the 17TRACK dashboard (Settings). The free plan is a **one-time 200 tracking numbers** for accounts created
after 2026-01-07, not monthly; after that it is paid. The panel's "Marcar" button remains the manual fallback.

## Adding a store

Copy `stores/arman-curren` to `stores/<id>`, edit `flow.yaml` and the templates. `-check` fails on bad YAML, unknown
triggers/conditions, `after_step` cycles, or a missing template. Then attach it to a tenant with
`mailflow tenant add --id <tenant> --name "<Name>" --store <id>` (or move an existing store by re-running it with the
new tenant), and invite its owner.

## Tests

`go test ./...` covers password hashing (argon2id format, wrong password, hostile cost parameters), the login rate
limit, session lifecycle (cookie flags, hashed storage, logout deleting the row, expiry), CSRF rejection, invite
single-use/expiry/minimum length, tenant isolation (a tenant-B user gets 404 on tenant-A store endpoints), and the
WebAuthn challenge rules. `internal/store` runs the same tenant/session/invite/passkey contract against both the
in-memory store and Postgres when `TEST_DATABASE_URL` is set.
