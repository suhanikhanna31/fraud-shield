# Fraud Shield

A real-time transaction fraud-scoring API written in Go, backed by PostgreSQL.

## Why this design

Fraud scoring needs to happen inline with a transaction, so the core engine
is built around cheap, well-understood data structures rather than a heavy
rules DB:

- **Sliding-window velocity check** — each account keeps a small slice of
  recent timestamps; timestamps outside the window are pruned on every
  call, giving amortized O(1) checks for "too many transactions too fast."
- **Union-Find (Disjoint Set Union)** — accounts that share a device ID or
  IP address are merged into the same set with path compression and union
  by rank, so a ring of coordinated fake accounts collapses to one
  connected component in near-constant time per operation.
- **Weighted rule engine** — each signal (large amount, high velocity,
  brand-new counterparty, linkage to a known-bad account) contributes a
  configurable weight to a 0–100 risk score; transactions scoring ≥ 50 are
  flagged and persisted as alerts.

## Project layout

```
cmd/server/         entrypoint (HTTP server wiring)
internal/models/    Transaction, Alert, Rule, ScoreResult types
internal/scoring/   the scoring engine (velocity, union-find, rules) + tests
internal/store/     Postgres + in-memory persistence, behind a Store interface
internal/api/       HTTP handlers (net/http, stdlib router)
schema.sql          Postgres schema
docker-compose.yml  local Postgres for development
```

## Running locally

Requires Go 1.22+.

```bash
# 1. start Postgres (optional — omit DATABASE_URL to run against an in-memory store)
make db-up

# 2. run the server
DATABASE_URL="postgres://fraudshield:fraudshield@localhost:5432/fraudshield?sslmode=disable" make run

# or, without Postgres at all:
make run
```

The server listens on `:8080` by default (override with `PORT`).

## API

### `POST /v1/transactions/score`
Score a transaction. Returns a risk score, whether it was flagged, and why.

```bash
curl -X POST localhost:8080/v1/transactions/score \
  -H "Content-Type: application/json" \
  -d '{
        "account_id": "acc_123",
        "amount": 15000,
        "currency": "USD",
        "device_id": "dev_abc",
        "counter_party": "acc_999"
      }'
```

```json
{
  "transaction_id": "3f1b...",
  "score": 35,
  "flagged": false,
  "reasons": [
    "transaction amount exceeds configured cap",
    "first-time transfer to this counterparty"
  ]
}
```

### `GET /v1/alerts?account_id=acc_123&limit=20`
List recently persisted alerts, optionally filtered by account.

### `GET /v1/rules` / `POST /v1/rules`
Inspect or update the weight/threshold/enabled state of a scoring rule.

### `POST /v1/ring/flag`
Mark an account as a confirmed fraud-ring participant; any account later
found to share a device or IP with it will be flagged via the union-find
linkage check.

### `GET /health`
Liveness/readiness probe (pings the database).

## Testing

```bash
make test
```

Covers: velocity-window pruning, union-find linkage across shared
device/IP, new-counterparty detection, and rule scoring thresholds.

## Notes / next steps

- Rules and thresholds currently live in memory; a production version
  would persist them in the `rules` table and hot-reload on change.
- The velocity and union-find state is per-process; a multi-instance
  deployment would move this into Redis or a shared cache.
- Authentication/authorization is intentionally out of scope for this
  scaffold — add middleware in `internal/api` before exposing publicly.
