# AI Repo Guardrails Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add strict local guardrails that keep AI-generated Go changes within the repository's architecture, dependency, lint, and formatting boundaries.

**Architecture:** Use three enforcement layers: `AGENTS.md` for human-readable policy, `golangci-lint` for Go quality rules, and repo-specific guardrails through a shell script plus Go architecture tests. The current codebase must be cleaned to pass the new baseline immediately.

**Tech Stack:** Go 1.26.2, golangci-lint 2.9.0, Bash, Go standard library tests.

---

## File Structure

- Modify `AGENTS.md`: add AI change discipline and required verification command.
- Create `.golangci.yml`: strict v2 golangci-lint configuration.
- Create `internal/architecture/architecture_test.go`: Go-native architecture and package allowlist tests.
- Create `scripts/repo-guard.sh`: single local guard command.
- Modify `internal/httpapi/router.go`: centralize route paths.
- Modify `internal/httpapi/health.go`: use centralized response constants.
- Modify `internal/httpapi/middleware.go`: centralize CORS header strings.
- Modify `internal/status/service.go`: add typed dependency status values and sentinel error.
- Modify `internal/status/handler.go`: use status constants in JSON responses.
- Modify `internal/status/*_test.go`, `internal/httpapi/router_test.go`: update tests to use constants/helpers and close response bodies with error checks.
- Modify `internal/app/app.go`: wrap returned external errors.
- Modify `internal/platform/postgres/postgres.go`: wrap pgx errors.

## Task 1: Document AI Change Discipline

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Append the policy section**

Add this section after "Security & Configuration Tips":

````markdown
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
````

- [ ] **Step 2: Verify the section is present**

Run:

```bash
rg -n "AI Change Discipline|repo-guard.sh|default-deny" AGENTS.md
```

Expected: each phrase is printed with a line number.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "Document AI change discipline"
```

## Task 2: Add Architecture Tests

**Files:**
- Create: `internal/architecture/architecture_test.go`

- [ ] **Step 1: Write package boundary tests**

Create `internal/architecture/architecture_test.go`:

```go
package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const modulePath = "orderbuddy-ai/backend"

var allowedPackages = []string{
	modulePath + "/cmd/server",
	modulePath + "/internal/app",
	modulePath + "/internal/architecture",
	modulePath + "/internal/config",
	modulePath + "/internal/httpapi",
	modulePath + "/internal/platform/postgres",
	modulePath + "/internal/status",
}

type packageInfo struct {
	ImportPath string
	Imports    []string
}

func TestPackagesRemainExplicitlyAllowed(t *testing.T) {
	packages := loadPackages(t)

	for _, pkg := range packages {
		if !slices.Contains(allowedPackages, pkg.ImportPath) {
			t.Fatalf("package %q is not in the architecture allowlist", pkg.ImportPath)
		}
	}
}

func TestPackageBoundaries(t *testing.T) {
	packages := loadPackages(t)

	assertDoesNotImport(t, packages, modulePath+"/internal/httpapi", modulePath+"/internal/platform/postgres")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/platform/postgres")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/app")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/platform/postgres")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/platform/postgres")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/status")
}

func loadPackages(t *testing.T) []packageInfo {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = "../.."

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		t.Fatalf("go list failed: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []packageInfo
	for {
		var pkg packageInfo
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode go list package: %v", err)
		}
		packages = append(packages, pkg)
	}

	return packages
}

func assertDoesNotImport(t *testing.T, packages []packageInfo, source string, forbidden string) {
	t.Helper()

	for _, pkg := range packages {
		if pkg.ImportPath != source {
			continue
		}
		if slices.Contains(pkg.Imports, forbidden) {
			t.Fatalf("%s must not import %s", source, forbidden)
		}
	}
}
```

- [ ] **Step 2: Run the architecture tests**

Run:

```bash
go test ./internal/architecture -run Test -v
```

Expected: PASS.

- [ ] **Step 3: Run all tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/architecture/architecture_test.go
git commit -m "Add architecture boundary tests"
```

## Task 3: Add Strict golangci-lint Configuration

**Files:**
- Create: `.golangci.yml`

- [ ] **Step 1: Add the linter configuration**

Create `.golangci.yml`:

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  default: none
  enable:
    - bodyclose
    - copyloopvar
    - cyclop
    - depguard
    - dupl
    - err113
    - errcheck
    - errname
    - errorlint
    - funlen
    - gocognit
    - goconst
    - gocritic
    - gomoddirectives
    - gomodguard
    - govet
    - iface
    - ineffassign
    - inamedparam
    - interfacebloat
    - ireturn
    - maintidx
    - nestif
    - nilerr
    - nilnil
    - nolintlint
    - revive
    - rowserrcheck
    - sqlclosecheck
    - staticcheck
    - unused
    - wrapcheck

  settings:
    cyclop:
      max-complexity: 8
      package-average: 4

    depguard:
      rules:
        main:
          list-mode: strict
          files:
            - "$all"
          allow:
            - $gostd
            - orderbuddy-ai/backend
            - github.com/gofiber/fiber/v3
            - github.com/jackc/pgx/v5

    dupl:
      threshold: 100

    errcheck:
      check-type-assertions: true
      check-blank: true

    errorlint:
      errorf: true
      asserts: true
      comparison: true

    funlen:
      lines: 60
      statements: 40
      ignore-comments: true

    gocognit:
      min-complexity: 12

    goconst:
      min-len: 2
      min-occurrences: 2
      ignore-tests: true
      ignore-functions:
        - fmt.Errorf
        - log.Printf
        - log.Fatalf
        - t.Fatalf
        - t.Fatal

    gomoddirectives:
      replace-local: false
      retract-allow-no-explanation: false
      toolchain-forbidden: false
      go-debug-forbidden: true
      check-module-path: false

    gomodguard:
      allowed:
        modules:
          - github.com/gofiber/fiber/v3
          - github.com/jackc/pgx/v5
      blocked:
        local-replace-directives: true

    interfacebloat:
      max: 3

    ireturn:
      allow:
        - error
        - stdlib
        - anon
        - generic

    maintidx:
      under: 20

    nolintlint:
      require-explanation: true
      require-specific: true
      allow-no-explanation: []
      allow-unused: false

    revive:
      severity: error
      rules:
        - name: blank-imports
        - name: context-as-argument
        - name: context-keys-type
        - name: dot-imports
        - name: early-return
        - name: error-naming
        - name: error-return
        - name: error-strings
        - name: exported
          disabled: true
        - name: increment-decrement
        - name: indent-error-flow
        - name: receiver-naming
        - name: redefines-builtin-id
        - name: superfluous-else
        - name: time-naming
        - name: unexported-return
        - name: unreachable-code
        - name: unused-parameter
        - name: var-declaration
        - name: var-naming

    wrapcheck:
      report-internal-errors: false
      extra-ignore-sigs:
        - .JSON(
        - .SendStatus(
        - .Next(

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

- [ ] **Step 2: Run lint to identify baseline failures**

Run:

```bash
golangci-lint run ./...
```

Expected: FAIL on current code before the next cleanup tasks. Expected categories include unwrapped external errors, unchecked response body close, repeated strings, and direct error creation.

- [ ] **Step 3: Commit only the lint config**

```bash
git add .golangci.yml
git commit -m "Add strict golangci-lint configuration"
```

## Task 4: Centralize HTTP and Status Constants

**Files:**
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/health.go`
- Modify: `internal/httpapi/middleware.go`
- Modify: `internal/httpapi/router_test.go`
- Modify: `internal/status/service.go`
- Modify: `internal/status/handler.go`
- Modify: `internal/status/service_test.go`
- Modify: `internal/status/handler_test.go`

- [ ] **Step 1: Centralize route paths in `internal/httpapi/router.go`**

Replace the file with:

```go
package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/status"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
	StatusPath  = "/api/status"
)

type RouterConfig struct {
	StatusHandler status.Handler
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New()
	app.Use(withCORS)
	app.Get(HealthzPath, healthz)
	app.Get(ReadyzPath, config.StatusHandler.Readyz)
	app.Get(StatusPath, config.StatusHandler.Status)
	return app
}
```

- [ ] **Step 2: Centralize HTTP response strings in `internal/httpapi/health.go`**

Replace the file with:

```go
package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const (
	responseStatusField = "status"
	responseStatusOK    = "ok"
)

func healthz(c fiber.Ctx) error {
	return writeJSON(c, http.StatusOK, fiber.Map{responseStatusField: responseStatusOK})
}
```

- [ ] **Step 3: Centralize CORS strings in `internal/httpapi/middleware.go`**

Replace the file with:

```go
package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const (
	headerAccessControlAllowOrigin  = "Access-Control-Allow-Origin"
	headerAccessControlAllowHeaders = "Access-Control-Allow-Headers"
	headerAccessControlAllowMethods = "Access-Control-Allow-Methods"
	headerContentType               = "Content-Type"
	corsAllowedOrigin               = "*"
	corsAllowedMethods              = "GET, OPTIONS"
)

func withCORS(c fiber.Ctx) error {
	c.Set(headerAccessControlAllowOrigin, corsAllowedOrigin)
	c.Set(headerAccessControlAllowHeaders, headerContentType)
	c.Set(headerAccessControlAllowMethods, corsAllowedMethods)

	if c.Method() == http.MethodOptions {
		return c.SendStatus(http.StatusNoContent)
	}

	return c.Next()
}
```

- [ ] **Step 4: Add typed dependency status values in `internal/status/service.go`**

Replace the file with:

```go
package status

import (
	"context"
	"errors"
	"time"
)

const serviceName = "ai-backend"

const (
	DependencyStatusOK    DependencyStatusValue = "ok"
	DependencyStatusError DependencyStatusValue = "error"
)

var ErrDatabaseMissing = errors.New("database is missing")

type DependencyStatusValue string

type Service struct {
	database DatabasePinger
}

type Response struct {
	Service     string           `json:"service"`
	Environment string           `json:"environment"`
	Database    DependencyStatus `json:"database"`
}

type DependencyStatus struct {
	Status DependencyStatusValue `json:"status"`
}

func NewService(database DatabasePinger) Service {
	return Service{database: database}
}

func (service Service) Ready(parent context.Context) error {
	return service.pingDatabase(parent)
}

func (service Service) Status(parent context.Context, environment string) Response {
	databaseStatus := DependencyStatusOK
	if service.pingDatabase(parent) != nil {
		databaseStatus = DependencyStatusError
	}

	return Response{
		Service:     serviceName,
		Environment: environment,
		Database: DependencyStatus{
			Status: databaseStatus,
		},
	}
}

func (service Service) pingDatabase(parent context.Context) error {
	if service.database == nil {
		return ErrDatabaseMissing
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	return service.database.Ping(ctx)
}
```

- [ ] **Step 5: Use typed status values in `internal/status/handler.go`**

Replace the file with:

```go
package status

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const responseStatusField = "status"

type Handler struct {
	service     Service
	environment string
}

func NewHandler(service Service, environment string) Handler {
	return Handler{service: service, environment: environment}
}

func (handler Handler) Readyz(c fiber.Ctx) error {
	if handler.service.Ready(c.Context()) != nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{responseStatusField: DependencyStatusError})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{responseStatusField: DependencyStatusOK})
}

func (handler Handler) Status(c fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(handler.service.Status(c.Context(), handler.environment))
}
```

- [ ] **Step 6: Update route tests to use constants and checked close**

In `internal/httpapi/router_test.go`, add this helper after imports:

```go
func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
```

Then replace `defer resp.Body.Close()` with:

```go
defer closeResponseBody(t, resp)
```

Replace hard-coded route paths with `HealthzPath` and `StatusPath`.

- [ ] **Step 7: Update status tests to use typed constants and checked close**

In `internal/status/handler_test.go`, add this helper after imports:

```go
func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
```

Replace every `defer resp.Body.Close()` with:

```go
defer closeResponseBody(t, resp)
```

Replace status route literals with package-local constants:

```go
const (
	readyzPath = "/readyz"
	statusPath = "/api/status"
)
```

Use `readyzPath` and `statusPath` in `app.Get` and `httptest.NewRequest`.

In `internal/status/service_test.go` and `internal/status/handler_test.go`, replace expected `"ok"` and `"error"` dependency status comparisons with `DependencyStatusOK` and `DependencyStatusError`.

- [ ] **Step 8: Format and test**

Run:

```bash
gofmt -w internal/httpapi/router.go internal/httpapi/health.go internal/httpapi/middleware.go internal/httpapi/router_test.go internal/status/service.go internal/status/handler.go internal/status/service_test.go internal/status/handler_test.go
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/httpapi internal/status
git commit -m "Centralize status and route constants"
```

## Task 5: Wrap External Errors

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/platform/postgres/postgres.go`

- [ ] **Step 1: Wrap app-level errors**

In `internal/app/app.go`, add `fmt` to imports and replace direct external returns:

```go
import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orderbuddy-ai/backend/internal/config"
	"orderbuddy-ai/backend/internal/httpapi"
	"orderbuddy-ai/backend/internal/platform/postgres"
	"orderbuddy-ai/backend/internal/status"
)
```

Change the postgres connection error block:

```go
if err != nil {
	return fmt.Errorf("connect postgres: %w", err)
}
```

Change the listen error select case:

```go
case err := <-listenErr:
	return fmt.Errorf("listen http: %w", err)
```

Change the shutdown return:

```go
if err := router.ShutdownWithContext(shutdownCtx); err != nil {
	return fmt.Errorf("shutdown http: %w", err)
}

return nil
```

- [ ] **Step 2: Wrap postgres errors**

In `internal/platform/postgres/postgres.go`, add `fmt` to imports and wrap pgx errors:

```go
import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)
```

Change parse, connect, and ping errors:

```go
config, err := pgxpool.ParseConfig(databaseURL)
if err != nil {
	return nil, fmt.Errorf("parse postgres config: %w", err)
}
```

```go
pool, err := pgxpool.NewWithConfig(ctx, config)
if err != nil {
	return nil, fmt.Errorf("create postgres pool: %w", err)
}
```

```go
if err := pool.Ping(ctx); err != nil {
	pool.Close()
	return nil, fmt.Errorf("ping postgres: %w", err)
}
```

- [ ] **Step 3: Format and test**

Run:

```bash
gofmt -w internal/app/app.go internal/platform/postgres/postgres.go
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go internal/platform/postgres/postgres.go
git commit -m "Wrap application and postgres errors"
```

## Task 6: Add Repo Guard Script

**Files:**
- Create: `scripts/repo-guard.sh`

- [ ] **Step 1: Create the guard script**

Create `scripts/repo-guard.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_command go
require_command golangci-lint

echo "checking gofmt"
unformatted="$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"
if [[ -n "${unformatted}" ]]; then
  echo "gofmt required for:" >&2
  echo "${unformatted}" >&2
  exit 1
fi

echo "checking direct dependencies"
direct_deps="$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all | sed '/^$/d' | sort)"
allowed_direct_deps="$(printf '%s\n' \
  'github.com/gofiber/fiber/v3' \
  'github.com/jackc/pgx/v5' \
  'orderbuddy-ai/backend' \
  | sort)"
unexpected_deps="$(comm -23 <(printf '%s\n' "${direct_deps}") <(printf '%s\n' "${allowed_direct_deps}"))"
if [[ -n "${unexpected_deps}" ]]; then
  echo "unexpected direct dependencies:" >&2
  echo "${unexpected_deps}" >&2
  exit 1
fi

echo "checking local replace directives"
if go mod edit -json | grep -q '"New":'; then
  if go mod edit -json | grep -A 3 '"Replace":' | grep -q '"Path": "\\.\\|/"'; then
    echo "local replace directives are not allowed" >&2
    exit 1
  fi
fi

echo "checking package allowlist"
packages="$(go list ./... | sort)"
allowed_packages="$(printf '%s\n' \
  'orderbuddy-ai/backend/cmd/server' \
  'orderbuddy-ai/backend/internal/app' \
  'orderbuddy-ai/backend/internal/architecture' \
  'orderbuddy-ai/backend/internal/config' \
  'orderbuddy-ai/backend/internal/httpapi' \
  'orderbuddy-ai/backend/internal/platform/postgres' \
  'orderbuddy-ai/backend/internal/status' \
  | sort)"
unexpected_packages="$(comm -23 <(printf '%s\n' "${packages}") <(printf '%s\n' "${allowed_packages}"))"
if [[ -n "${unexpected_packages}" ]]; then
  echo "unexpected packages:" >&2
  echo "${unexpected_packages}" >&2
  exit 1
fi

echo "checking temporary files"
if find . \
  -path './.git' -prune -o \
  -type f \( -name '*.tmp' -o -name '*.bak' -o -name 'coverage.out' -o -name '*.test' \) \
  -print | grep -q .; then
  echo "temporary or generated files are present" >&2
  find . \
    -path './.git' -prune -o \
    -type f \( -name '*.tmp' -o -name '*.bak' -o -name 'coverage.out' -o -name '*.test' \) \
    -print >&2
  exit 1
fi

echo "running tests"
go test ./...

echo "running golangci-lint"
golangci-lint run ./...

echo "repo guard passed"
```

- [ ] **Step 2: Make the script executable**

Run:

```bash
chmod +x scripts/repo-guard.sh
```

- [ ] **Step 3: Run the guard and capture expected failures**

Run:

```bash
./scripts/repo-guard.sh
```

Expected before final lint cleanup: it may fail at `golangci-lint run ./...`. It must not fail because the script is missing commands, has syntax errors, or mis-detects current direct dependencies.

- [ ] **Step 4: Commit**

```bash
git add scripts/repo-guard.sh
git commit -m "Add repository guard script"
```

## Task 7: Verify Strict Lint Baseline

**Files:**
- No planned source changes.

- [ ] **Step 1: Run lint**

Run:

```bash
golangci-lint run ./...
```

Expected: PASS.

- [ ] **Step 2: Stop if lint fails**

If lint fails, stop execution and report the exact output. Do not weaken `.golangci.yml`, add `//nolint`, or make broad cleanup changes without revising this plan.

- [ ] **Step 3: Verify tests pass**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Verify formatting is clean**

Run:

```bash
test -z "$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"
```

Expected: PASS.

- [ ] **Step 5: Commit if verification changed no files**

Run:

```bash
git status --short
```

Expected: no output. No commit is needed for this task.

## Task 8: Final Guard Verification

**Files:**
- No planned source changes.

- [ ] **Step 1: Run the full guard**

Run:

```bash
./scripts/repo-guard.sh
```

Expected: PASS and final line `repo guard passed`.

- [ ] **Step 2: Check worktree status**

Run:

```bash
git status --short
```

Expected: no output.

- [ ] **Step 3: Record final verification in the task handoff**

Report these command results:

```text
go test ./...: PASS
golangci-lint run ./...: PASS
./scripts/repo-guard.sh: PASS
```

No commit is needed in this task if the worktree is already clean.

## Self-Review

- Spec coverage: `AGENTS.md` policy is in Task 1; strict lint is in Task 3; architecture tests are in Task 2; repo guard is in Task 6; current-code cleanup and final verification are in Tasks 4, 5, 7, and 8.
- Placeholder scan: the plan contains no deferred requirements or undefined future work.
- Type consistency: the plan uses `DependencyStatusValue`, `DependencyStatusOK`, and `DependencyStatusError` consistently across service, handler, and tests.
- Scope check: the plan does not add CI, git hooks, CODEOWNERS, branch protection, cloud agent configuration, or new production packages.
