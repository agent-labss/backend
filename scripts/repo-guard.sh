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
  'github.com/openai/openai-go/v3' \
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
  'orderbuddy-ai/backend/internal/agent' \
  'orderbuddy-ai/backend/internal/app' \
  'orderbuddy-ai/backend/internal/architecture' \
  'orderbuddy-ai/backend/internal/config' \
  'orderbuddy-ai/backend/internal/httpapi' \
  'orderbuddy-ai/backend/internal/platform/postgres' \
  'orderbuddy-ai/backend/internal/status' \
  'orderbuddy-ai/backend/internal/toolcatalog' \
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
