# Paperboat specification

Paperboat is the managed workload: a fish rental business. Customers rent live fish. The
business delivers the fish and collects them when the rental ends. This file is the interface
between Paperboat and the rest of the repository.

## 1. Build and packaging

- Go, standard library plus one Postgres driver (`github.com/jackc/pgx/v5`). Module path
  `github.com/statik/site-reliability-clanker/apps/paperboat` with its own `go.mod`.
- One static binary, `paperboat`, with subcommands `api`, `worker`, `migrate`, `loadgen`, `web`.
- One image for each release: `<registry>/paperboat:<version>`. `CGO_ENABLED=0`, distroless
  static base, non-root user.
- `apps/paperboat/build.sh <registry>` builds and pushes all four releases. Each release is a
  directory or build argument, not a git branch, so one checkout builds all of them.
- Helm chart in `apps/paperboat/chart`. The chart values are in `platform/CONTRACT.md` section 4.

## 2. Services

| Subcommand | Kubernetes object | Port | Role |
|---|---|---|---|
| `web` | Deployment + Service | 8080 | Serves the browser UI and proxies `/api/` to `api`. This is the public entry point: the HTTPRoute of layer `50-app` sends each request on the Gateway of the platform to its `ClusterIP` Service |
| `api` | Deployment + Service | 8081 | REST API and the live inventory stream |
| `worker` | Deployment | — | Fish aging, delivery progress, manifest files, support escalation |
| `migrate` | Job, Helm `pre-install,pre-upgrade` hook | — | Applies schema migrations, then exits |
| `loadgen` | Deployment, optional | — | Synthetic customers against `web` |

`api` and `worker` mount the `ReadWriteMany` claim at `/data`. `worker` writes delivery
manifests and health certificates there. `api` serves them.

Configuration is environment variables only: `DATABASE_URL` (from the secret), `DATA_DIR`
(default `/data`), `API_URL` (for `web` and `loadgen`), `ADMIN_TOKEN` (optional).

## 3. Domain

| Entity | Fields that matter |
|---|---|
| Species | name, daily price, `retire_after_days` (from 1.1.0) |
| Tank | name, location, capacity, water temperature |
| Fish | name, species, tank, hatch date, status, `condition` score 0–100 (from 1.1.0) |
| Customer | name, email, address |
| Rental | customer, fish, start, end, status (`reserved`, `active`, `returned`, `cancelled`), price |
| Delivery | rental, kind (`delivery`, `collection`), status (`scheduled`, `out`, `done`, `failed`), driver, window, manifest path |
| Support ticket | customer, optional rental or delivery, subject, status (`open`, `waiting`, `resolved`), priority |
| Support interaction | ticket, author, channel (`phone`, `email`, `chat`), body, time |

Fish status values: `available`, `reserved`, `rented`, `in_transit`, `quarantined`, `retired`.

`migrate` seeds a starting stock (about 8 species, 6 tanks, 60 fish, 20 customers) when the
database is empty.

## 4. HTTP interface

All paths are served by `api` and reachable through `web` at the same path.

| Method and path | Purpose |
|---|---|
| `GET /api/healthz` | `200` with `{"status":"ok"}` when the database answers. `503` otherwise |
| `GET /api/version` | `{"version":"1.0.0","schema_version":1,"base_image":"..."}` |
| `GET /api/stats` | `{"requests":n,"errors":n,"p95_ms":n,"fish_available":n,"rentals_active":n,"deliveries_out":n,"tickets_open":n}`. Counters since process start |
| `GET /api/inventory` | Fish with species, tank, status, age in days, condition |
| `GET /api/inventory/stream` | Server-sent events. One event for each change of fish status |
| `GET /api/tanks` | Tanks with occupancy |
| `GET,POST /api/rentals`, `POST /api/rentals/{id}/return` | Rentals. A new rental reserves the fish and schedules a delivery |
| `GET /api/deliveries`, `POST /api/deliveries/{id}/status` | Deliveries |
| `GET /api/deliveries/{id}/manifest` | The manifest file from the shared volume |
| `GET,POST /api/tickets`, `GET,POST /api/tickets/{id}/interactions` | Customer support |
| `GET /api/customers` | Customers |
| `POST /api/admin/fault` | Fault injection for the incident scenario. Requires `Authorization: Bearer $ADMIN_TOKEN`. Body `{"mode":"none"|"slow"|"errors"}`. The fault is in memory only; a restart clears it |

Errors are JSON: `{"error":"message"}`. Each request writes one structured JSON log line to
stdout.

## 5. Worker jobs

| Job | Interval | Effect |
|---|---|---|
| Aging | 30 s | Updates condition (from 1.1.0). Retires fish older than the limit of their species (from 1.1.0). Returns quarantined fish to `available` |
| Delivery progress | 10 s | Moves deliveries `scheduled` → `out` → `done`. Sets the fish `in_transit`, then `rented` or `available`. Writes the manifest to `/data/manifests/` |
| Support escalation | 30 s | Raises the priority of tickets that stay open too long |
| Health certificates | 60 s | Writes a certificate file for each fish to `/data/certificates/` |

`loadgen` creates rentals, returns them and opens support tickets at a slow, steady rate, so
inventory moves without a human.

## 6. Releases

| Version | Content | Schema version |
|---|---|---|
| `1.0.0` | Inventory, rentals, deliveries, customer support. Age is shown, not acted on | 1 |
| `1.1.0` | Fish aging: condition score and automatic retirement. Migration adds `species.retire_after_days` and `fish.condition`. UI shows aging | 2 |
| `1.1.1` | Security patch: newer base image. No functional change. `base_image` in `/api/version` changes | 2 |
| `1.2.0-bad` | Deliberate fault: `GET /api/inventory` returns `500` for about half of the requests. Health and readiness checks still pass | 2 |

Migrations are forward-only and idempotent. `1.2.0-bad` → `1.1.1` needs no schema change.

Each release has a release note in `apps/paperboat/releases/<version>.md` with a one-line
summary, the changes, the migration and the risk. The Author and Explainer roles read it.

## 7. Browser UI

Server-rendered HTML with a small amount of JavaScript, no build step, embedded in the binary.
Pages: live inventory board (updates from the stream, grouped by tank, shows age and
condition), rentals, deliveries, support desk. The footer shows the version.
