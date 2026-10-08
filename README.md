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
  brand-new counterparty, linkage to a known-bad account) is an independent
  `Evaluator` that judges precomputed `Signals`, so every rule is a pure
  function you can unit test without an `Engine`. Each contributes a
  configurable weight to a 0–100 risk score; transactions scoring ≥ 50 are
  flagged and persisted as alerts. Weights, thresholds and enabled flags are
  changeable at runtime via `POST /v1/rules`, and new rules plug in with
  `Engine.RegisterEvaluator`.

## Project layout

```
cmd/server/            entrypoint (HTTP server wiring, graceful shutdown)
internal/models/       Transaction, Alert, Rule, ScoreResult types
internal/scoring/      scoring.go, rules.go, velocity.go, unionfind.go + tests
internal/store/        Postgres + in-memory persistence behind a Store interface (+ auto-migrate)
internal/api/          HTTP handlers (net/http, stdlib router) + httptest tests
schema.sql             Postgres schema (also embedded and auto-applied on startup)
docker-compose.yml     local Postgres for development
Dockerfile             multi-stage build, non-root (OpenShift-compatible)
deploy/openshift/      ImageStream, BuildConfig, Postgres, Deployment, Service, Route
ansible/               deploy.yml (build + deploy) and teardown.yml
render.yaml            Render Blueprint (web service + managed Postgres)
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
make test         # all tests
make test-race    # with the race detector
make cover        # coverage summary
```

Covers: each rule in isolation, velocity-window pruning and bounded memory,
union-find (transitive connectivity, path compression, union by rank, device/IP
clustering), engine scoring/clamping/runtime reconfiguration, the in-memory
store, and every HTTP endpoint via `httptest`.

The PostgreSQL store runs the same contract test as the in-memory store when a
database is available (skipped otherwise):

```bash
make db-up
DATABASE_URL="postgres://fraudshield:fraudshield@localhost:5432/fraudshield?sslmode=disable" \
  go test ./internal/store -v
```

## Docker

```bash
make docker-run     # builds the image and serves on :8080 (in-memory store)
```

## Deploy to OpenShift (Developer Sandbox)

The Ansible playbook builds the image in-cluster from your working tree
(BuildConfig → ImageStream), deploys PostgreSQL (PVC-backed) and the API, and
smoke-tests `/health` through the Route.

**Prerequisites (macOS):**

```bash
brew install openshift-cli ansible
```

**Deploy:**

```bash
# 1. In the Sandbox web console: click your username (top right) ->
#    "Copy login command" -> "Display Token", then paste the `oc login ...` line.
oc login --token=<token> --server=<server>
oc project            # confirm it is your "<username>-dev" project

# 2. Pick a database password and deploy
export DB_PASSWORD='choose-a-password'
make deploy-openshift          # = cd ansible && ansible-playbook deploy.yml
```

The playbook prints the public URL at the end. You can also fetch it later:

```bash
oc get route fraud-shield -o jsonpath='https://{.spec.host}{"\n"}'
```

**Try it:**

```bash
URL=$(oc get route fraud-shield -o jsonpath='https://{.spec.host}')
curl $URL/health
curl -X POST $URL/v1/transactions/score \
  -H "Content-Type: application/json" \
  -d '{"account_id":"acc_123","amount":15000,"counter_party":"acc_999"}'
```

**Operate:**

```bash
oc get pods                            # status
oc logs deployment/fraud-shield        # API logs
make deploy-openshift                  # rebuild + redeploy after code changes
make teardown-openshift                # remove everything, including the DB volume
```

Set `openshift_namespace` in `ansible/group_vars/all.yml` to target a specific
project; by default the current `oc project` is used. Manifests live in
`deploy/openshift/` and can also be applied manually with
`oc apply -k deploy/openshift` (create the Secret first; see
`secret.example.yaml`).

The Sandbox may scale idle workloads down. If the URL stops responding, run
`oc get pods`, and re-run `make deploy-openshift` if the pods are gone.

## Deploy to Render

Push the repo to GitHub, then in Render choose **New → Blueprint** and select
it. `render.yaml` provisions a Docker web service and a managed Postgres and
wires `DATABASE_URL`; the schema is applied automatically on first start.

## Notes / next steps

- Rules and thresholds currently live in memory; a production version
  would persist them in the `rules` table and hot-reload on change.
- The velocity and union-find state is per-process; a multi-instance
  deployment would move this into Redis or a shared cache.
- Authentication/authorization is intentionally out of scope for this
  scaffold — add middleware in `internal/api` before exposing publicly.
