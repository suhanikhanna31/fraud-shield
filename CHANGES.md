# What was added to the original repo

Everything the project description claims that was not in `fraud-shield-main`.

## New
| Path | Why |
|---|---|
| `deploy/openshift/*.yaml` | Deployment, Service, Route (+ ImageStream, BuildConfig, Postgres, Secret template, kustomization) |
| `ansible/` | `deploy.yml` automates build + deploy; `teardown.yml` removes it |
| `render.yaml` | Render Blueprint ("live on Render") |
| `.dockerignore` | Smaller build context |
| `internal/scoring/rules.go` | Rules split out as independent `Evaluator`s |
| `internal/scoring/velocity.go`, `unionfind.go` | Algorithms split into their own files |
| `internal/scoring/{rules,velocity,unionfind,engine}_test.go` | Unit tests for each rule and algorithm |
| `internal/store/migrate.go`, `store_test.go` | Auto-apply schema; Store contract test (memory + Postgres) |
| `internal/api/handlers_test.go` | httptest coverage of every endpoint |

## Changed
- `scoring.go`: engine now iterates registered `Evaluator`s instead of inline `if` blocks. Public API unchanged; original tests pass untouched. Velocity max is now the `velocity` rule's `Threshold` (adjustable via `POST /v1/rules`); `Config.FlagThreshold` added (default 50). Reasons are `[]` not `null` when empty.
- `velocity`: pruning now copies survivors so expired timestamps are actually freed.
- `main.go`: applies schema on startup; graceful shutdown on SIGTERM.
- `handlers.go`: `GET /v1/alerts` returns `[]` instead of `null`; request bodies capped at 1 MiB.
- `Dockerfile`: stripped static binary, numeric non-root `USER` (required for OpenShift's random-UID policy).
- `Makefile`: added `test-race`, `cover`, `vet`, `docker-*`, `*-openshift` targets.
