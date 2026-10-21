# mailflow

Post-purchase email journey for our Shopify stores (Arman Curren first). Go, Postgres, no framework.
Shopify webhooks -> scheduled jobs in Postgres -> SMTP, with a panel that shows the flow as lanes of step cards.

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
human (click the red chip in the panel). Jobs found in `sending` after a restart are marked `failed`, never resent.
`UNIQUE(order_id, step_id)` plus idempotent webhooks mean a redelivered `orders/paid` cannot send twice.

## Run

```bash
go test ./...                                     # unit + contract tests
TEST_DATABASE_URL=postgres://... go test ./...    # also runs the Postgres contract
go run ./cmd/mailflow -check                      # validate flows + templates
ADMIN_PASSWORD=dev go run ./cmd/mailflow          # in-memory store, dry-run sender, http://127.0.0.1:8080 (admin/dev)
```

Env: `TRACK17_KEY` (17TRACK; unset = no automatic carrier events), `DATABASE_URL` (unset = memory), `SMTP_HOST/PORT/USER/PASS` (unset = dry-run log sender), `ADMIN_PASSWORD`,
`SHOPIFY_WEBHOOK_SECRET_<STORE_ID>` (`ARMAN_CURREN`), `STORES_DIR`, `LISTEN_ADDR`.

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
triggers/conditions, `after_step` cycles, or a missing template.
