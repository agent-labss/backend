# Repository Guidelines

## Project Structure & Module Organization

This is a Go backend module, `orderbuddy-ai/backend`. The executable entry point is `cmd/server/main.go`, which loads configuration and starts the app. Internal packages live under `internal/`: `app` wires dependencies and graceful shutdown, `config` reads environment settings, `httpapi` defines Fiber routes and middleware, `platform/postgres` owns PostgreSQL connectivity, and `status` implements health/readiness behavior. Tests sit beside the code they cover as `*_test.go`. Design notes and implementation plans are in `docs/superpowers/`.

## Build, Test, and Development Commands

- `go test ./...`: run the full test suite.
- `go test ./internal/status`: run one package while iterating.
- `go run ./cmd/server`: start the HTTP server locally.
- `gofmt -w <files>`: format changed Go files before review.
- `go mod tidy`: clean module requirements after dependency changes.

Local defaults are defined in `internal/config/config.go`: `HTTP_ADDR=:8080`, `APP_ENV=development`, and a local PostgreSQL `DATABASE_URL`. Override them with environment variables, for example `HTTP_ADDR=:9090 go run ./cmd/server`.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package names short, lowercase, and domain-oriented, matching the existing `httpapi`, `config`, and `status` style. Export only cross-package types and functions; keep helpers unexported. Prefer explicit constructor functions such as `NewService` and `NewHandler` when wiring dependencies. Test names should describe behavior, as in `TestLoadUsesEnvironmentOverrides` or `TestHandlerReadyzReturnsServiceUnavailableWhenDatabaseFails`.

## Testing Guidelines

The project uses Go's built-in `testing` package plus Fiber's `app.Test` for HTTP handlers. Add unit tests next to modified packages and use small fakes for external dependencies, as the status and router tests do. Cover success and failure paths for handlers, configuration, database connectivity boundaries, and readiness checks. Run `go test ./...` before submitting changes.

## Commit & Pull Request Guidelines

This repository currently has no commit history, so no project-specific commit convention is established. Use concise imperative subjects such as `Add readiness handler` or `Wire postgres status checks`. Pull requests should include a short summary, tests run, linked issue or task context when available, and API or configuration changes such as new routes or environment variables.

## Security & Configuration Tips

Do not commit real database credentials. Keep secrets in environment variables and document new required settings near the code that reads them. Preserve readiness behavior so `/readyz` fails when PostgreSQL is unavailable, while `/healthz` remains a lightweight process check.

## AI Change Discipline

This repository uses strict local guardrails for AI-assisted coding. Treat these as default-deny rules.

Do not add a new direct dependency, new `internal/` package, new architectural layer, new interface, or new enum-like status value unless you first explain the need, alternatives, and blast radius, then receive approval.

Keep package ownership intact:

- `cmd/server` loads config and calls `app.Run`.
- `internal/app` wires config, postgres, status, and HTTP routing.
- `internal/config` reads environment settings only.
- `internal/httpapi` owns Fiber routes, middleware, and HTTP helpers.
- `internal/platform/postgres` owns PostgreSQL connectivity only.
- `internal/status` owns health/readiness/status behavior only.

Do not bypass package boundaries. In particular, HTTP handlers must not import `internal/platform/postgres`, status logic must not import HTTP or postgres packages, and platform packages must not import application or HTTP packages.

Avoid stringly typed behavior. Route paths, repeated JSON field names, status strings, and enum-like values must be constants or typed constants in the package that owns them.

Every code change must finish by running:

```bash
./scripts/repo-guard.sh
```

If the guard fails, the change is not complete. Fix the failure or discuss the exception before proceeding.
