# Repository Guidelines

## Project Structure & Module Organization

This is a Go backend module, `ai/backend`. The executable entry point is `cmd/server/main.go`, which loads configuration and starts the app. Internal packages live under `internal/`: `app` wires dependencies and graceful shutdown, `config` reads environment settings, `httpapi` defines Fiber routes and middleware, `database` owns GORM models and generated query code, `platform/datastore` selects the configured database driver, `platform/sqlite` owns SQLite connectivity and migration, `toolcatalog` owns registered tool metadata and instructions, `agent` owns LLM orchestration and CLI execution, and `status` implements status behavior. Tests sit beside the code they cover as `*_test.go`. Design notes and implementation plans are in `docs/superpowers/`.

## Build, Test, and Development Commands

- `go test ./...`: run the full test suite.
- `go test ./internal/status`: run one package while iterating.
- `go run ./cmd/server`: start the HTTP server locally.
- `./scripts/generate-sqlite-db.sh [path]`: create or migrate a SQLite database file; defaults to `sqlite.db`.
- `gofmt -w <files>`: format changed Go files before review.
- `go mod tidy`: clean module requirements after dependency changes.
- `go run gorm.io/cli/gorm@v0.2.4 gen -i internal/database/queryinput/queries.go -o internal/database/generated`: regenerate typed GORM query helpers after changing query input interfaces.

Local defaults are defined in `internal/config/config.go`: `HTTP_ADDR=:8080`, `APP_ENV=development`, `DATABASE_DRIVER=sqlite`, and `DATABASE_URL=sqlite.db`. The app reads `.env` from the current working directory, and real environment variables override `.env` values. Start from `.env.example`, then run `go run ./cmd/server`.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package names short, lowercase, and domain-oriented, matching the existing `httpapi`, `config`, and `status` style. Export only cross-package types and functions; keep helpers unexported. Prefer explicit constructor functions such as `NewService` and `NewHandler` when wiring dependencies. Test names should describe behavior, as in `TestLoadUsesEnvironmentOverrides` or `TestHandlerStatusReturnsDatabaseErrorWithOKHTTPStatus`.

## Testing Guidelines

The project uses Go's built-in `testing` package plus Fiber's `app.Test` for HTTP handlers. Add unit tests next to modified packages and use small fakes for external dependencies, as the status and router tests do. Cover success and failure paths for handlers, configuration, database connectivity boundaries, and status checks. Run `go test ./...` before submitting changes.

## Commit & Pull Request Guidelines

This repository currently has no commit history, so no project-specific commit convention is established. Use concise imperative subjects such as `Add status handler` or `Wire sqlite status checks`. Pull requests should include a short summary, tests run, linked issue or task context when available, and API or configuration changes such as new routes or environment variables.

## Security & Configuration Tips

Do not commit real database files, credentials, or secrets. Keep secrets in environment variables and document new required settings near the code that reads them. Keep `/api/status` reporting the configured SQLite database status accurately.

## Database & GORM CLI Notes

SQLite is the only supported database driver today. GORM models live in `internal/database/models.go`; `internal/platform/sqlite` opens the database and runs `AutoMigrate(database.Models()...)`.

Typed query helpers are generated with GORM CLI. Edit `internal/database/queryinput/queries.go` for raw SQL query interfaces, then regenerate `internal/database/generated` with:

```bash
go run gorm.io/cli/gorm@v0.2.4 gen -i internal/database/queryinput/queries.go -o internal/database/generated
```

Do not hand-edit generated query code unless you are deliberately removing stale generated output and have updated the query input source in the same change. Keep custom SQL in the query input package and general create/update operations on GORM's typed API where no custom query is needed.

Production code outside `internal/database/generated` must not use hand-written GORM query entry points such as `Where("...")`, `Order("...")`, `First(&...)`, `Find(&...)`, `Scan(&...)`, `Save(&...)`, or raw `Exec(ctx, ...)` SQL. Add or change SQL in `internal/database/queryinput/queries.go`, regenerate `internal/database/generated`, and call the generated query methods from owning packages. Because GORM CLI raw SQL methods scan into zero values when no rows match, repository code must preserve domain not-found behavior explicitly after generated lookups.

## AI Change Discipline

This repository uses strict local guardrails for AI-assisted coding. Treat these as default-deny rules.

Do not add a new direct dependency, new `internal/` package, new architectural layer, new interface, or new enum-like status value unless you first explain the need, alternatives, and blast radius, then receive approval.

Keep package ownership intact:

- `cmd/server` loads config and calls `app.Run`.
- `internal/app` wires config, database, status, tool catalog, agent, and HTTP routing.
- `internal/config` reads environment settings only.
- `internal/database` owns GORM models, JSON database value helpers, GORM CLI query inputs, and generated query helpers only.
- `internal/httpapi` owns Fiber routes, middleware, and HTTP helpers.
- `internal/platform/datastore` owns database driver selection, close, and ping adapters only.
- `internal/platform/sqlite` owns SQLite connectivity and migration only.
- `internal/status` owns status behavior only.
- `internal/toolcatalog` owns tool metadata, trusted command validation, instruction storage, and related handlers/services only.
- `internal/agent` owns agent runs, OpenAI planning, CLI execution, redaction, run context, audit persistence, and related handlers/services only.

Do not bypass package boundaries. In particular, HTTP routing helpers must not import database platform packages, status logic must not import HTTP or platform packages, domain packages must not import `internal/platform/datastore` or `internal/platform/sqlite`, and platform packages must not import application, HTTP, status, agent, or tool catalog packages.

Avoid stringly typed behavior. Route paths, repeated JSON field names, status strings, and enum-like values must be constants or typed constants in the package that owns them.

Every code change must finish by running:

```bash
./scripts/repo-guard.sh
```

If the guard fails, the change is not complete. Fix the failure or discuss the exception before proceeding.
