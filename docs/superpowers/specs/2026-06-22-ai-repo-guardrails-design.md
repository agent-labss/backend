# AI Repo Guardrails Design

Date: 2026-06-22

## Context

This repository is a small Go backend module, `ai/backend`. It has a deliberately simple structure:

- `cmd/server` loads configuration and starts the application.
- `internal/app` wires dependencies and graceful shutdown.
- `internal/config` reads environment configuration.
- `internal/httpapi` owns Fiber routes and HTTP middleware.
- `internal/database` owns GORM models, GORM CLI query inputs, and generated query helpers.
- `internal/platform/datastore` owns database driver selection, close, and ping adapters.
- `internal/platform/sqlite` owns SQLite connectivity and migration.
- `internal/status` owns status behavior.

The main risk is not syntax errors. The risk is local AI coding sessions producing maintainability debt: unnecessary dependencies, premature abstractions, package sprawl, stringly typed domain values, scattered constants, and cross-layer imports that make the repo harder to reason about.

The user has already adopted the `superpowers` workflow for interaction discipline, so this design does not solve approval prompts or planning gates. It focuses on repo-level rules and automated checks for local CLI/IDE AI coding.

## External Basis

The design follows these documented practices and observations:

- GitHub Copilot repository custom instructions support repository-wide, path-specific, and agent instruction files such as `AGENTS.md`. Their purpose is to give agents project-specific build, validation, and architecture context.
- Claude Code settings and permissions support project-level configuration, hooks, and permission rules; this supports the broader principle that agent behavior should be constrained by repository-local policy, not only by prompt text.
- `golangci-lint` provides linters for dependency control, duplicated strings, error handling, interface discipline, module directives, complexity, and `nolint` hygiene.
- Go Code Review Comments and the Uber Go Style Guide emphasize idiomatic Go, small interfaces, clear error handling, `gofmt`, avoiding unnecessary globals and `init`, and keeping code simple.
- Practical LLM coding guidance, including Simon Willison's writing, emphasizes explicit context, strong human direction, and testing everything the model writes.

## Goals

- Make AI-generated changes fail fast when they violate repository discipline.
- Keep the Go backend simple and idiomatic.
- Prevent unapproved third-party dependencies and local module replacements.
- Prevent unapproved package growth under `internal/`.
- Prevent cross-layer imports that break the current architecture.
- Discourage stringly typed status values, route paths, and scattered constants.
- Require strict lint, tests, and formatting from the current codebase as the baseline.

## Non-Goals

- Do not configure GitHub branch protection, CODEOWNERS, or cloud agent behavior in this design.
- Do not introduce git hooks or CI in the first implementation.
- Do not redesign the application architecture.
- Do not add a framework, code generator, or large custom static analysis system.
- Do not make the repo hostile to ordinary Go changes; exceptions remain possible after explicit approval.

## Key Policy

The policy is "default deny, explicit exception."

AI agents must not add any of the following without first explaining the need, alternatives, and blast radius, then receiving approval:

- New direct dependencies.
- New packages under `internal/`.
- New architectural layers such as manager, provider, registry, adapter, mapper, or generic repository abstractions.
- New interfaces unless they are consumer-side, small, and immediately needed for testing or dependency inversion.
- New domain status strings, route path strings, or enum-like values scattered across packages.
- Cross-layer imports that bypass the existing package boundaries.

## Architecture

The guardrail system has three layers.

### 1. Behavior Rules: `AGENTS.md`

`AGENTS.md` remains the human-readable rule source. It will gain an "AI Change Discipline" section that documents:

- The default-deny policy.
- Approved package ownership boundaries.
- Dependency approval rules.
- String and const discipline.
- Required verification command.
- Exception process.

This layer guides AI before it writes code. It is not trusted as the only enforcement mechanism.

### 2. Code Police: `.golangci.yml`

`golangci-lint` enforces general Go quality and AI-specific failure modes.

Recommended linter groups:

- Correctness: `govet`, `staticcheck`, `unused`, `ineffassign`, `errcheck`, `bodyclose`, `rowserrcheck`, `sqlclosecheck`, `copyloopvar`.
- AI pollution control: `goconst`, `gocritic`, `revive`, `gocognit`, `cyclop`, `funlen`, `dupl`, `nestif`, `maintidx`.
- Dependency control: `depguard`, `gomoddirectives`, `gomodguard_v2`.
- Interface discipline: `iface`, `interfacebloat`, `ireturn`, `inamedparam`.
- Error discipline: `errorlint`, `errname`, `err113`, `nilerr`, `nilnil`, `wrapcheck`.
- Suppression discipline: `nolintlint`.

Current direct dependency allowlist:

- `github.com/gofiber/fiber/v3`
- `github.com/openai/openai-go/v3`
- `gorm.io/cli/gorm`
- `gorm.io/driver/sqlite`
- `gorm.io/gorm`

Every `//nolint` must name the specific linter and include a reason.

### 3. Repo Boundary Checks: Script and Architecture Tests

`scripts/repo-guard.sh` will be the single local verification command. It will run:

- `gofmt` cleanliness check.
- `go test ./...`.
- `golangci-lint run ./...`.
- `go.mod` checks for direct dependency allowlist and local `replace`.
- Package allowlist checks for `internal/`.
- Temporary/generated file blacklist checks.

`internal/database/generated` is intentionally committed generated code from GORM CLI, with `internal/database/queryinput` as its source interface package. It is part of the package allowlist; ad hoc generated or temporary files remain blocked.

Architecture tests will be Go tests, so `go test ./...` catches architecture violations. They will validate:

- `internal/httpapi` does not import database platform packages.
- `internal/status` does not import `internal/httpapi` or database platform packages.
- Database platform packages do not import `internal/httpapi`, `internal/status`, `internal/agent`, or `internal/toolcatalog`.
- `internal/config` does not import app, HTTP, status, or platform packages.
- `cmd/server` only handles startup-level orchestration and does not directly wire router, database, or status internals.
- The allowed package list is explicit and must be updated when adding packages.

## Validation Flow

For any future AI code change:

1. The agent reads `AGENTS.md`.
2. If the task requires an exception, the agent asks for approval before editing.
3. The agent makes a narrow change.
4. The agent runs `./scripts/repo-guard.sh`.
5. The agent reports changed files, lint/test results, and any approved exceptions.

If `repo-guard.sh` fails, the change is not complete. The failure must be fixed or explicitly discussed.

## Error Handling

The guardrail tooling should fail closed:

- Missing `golangci-lint` produces a clear installation error and a non-zero exit.
- `go list` or `go test` failures stop the script.
- Unknown direct dependencies fail the dependency check.
- Unknown packages fail the package allowlist check.
- `//nolint` without a reason fails lint.

The only accepted bypass is an intentional code change that updates the relevant allowlist or lint configuration with an explanation in the same change.

## Testing Strategy

The implementation must establish a clean baseline:

- Current code must pass `go test ./...`.
- Current code must pass `golangci-lint run ./...`.
- Current code must pass `./scripts/repo-guard.sh`.
- Architecture tests must include at least one test that scans package import data and fails with actionable messages.

Because this repo is small, existing code should be cleaned to the strict rules immediately rather than grandfathering old issues.

## Expected Implementation Files

- Update `AGENTS.md`.
- Add `.golangci.yml`.
- Add `scripts/repo-guard.sh`.
- Add architecture tests under `internal/architecture/` or another internal test package dedicated to repository policy.
- Adjust existing Go code only as required to satisfy the new baseline.

## Open Decisions Already Resolved

- Scope is local CLI/IDE AI coding, not cloud agent governance.
- Interaction approval is handled by `superpowers`.
- Enforcement level is strict.
- Existing code must be cleaned immediately rather than using a legacy baseline.
- The chosen approach is lint plus guard script plus architectural tests.
