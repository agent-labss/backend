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

forbidden_database_pattern() {
  local file="$1"
  grep -Eq '("ai/backend/internal/database/generated"|"gorm.io/gorm"|"gorm.io/cli/gorm/field"|"gorm.io/cli/gorm/typed")' "${file}" || return 1
  grep -Eq '(\.(Clauses|Count|Delete|Exec|Find|First|Last|Model|Order|Raw|Row|Rows|Save|Scan|Table|Take|Update|Updates|Where)\(|gorm\.WithResult\(|gorm\.io/cli/gorm/(field|typed))' "${file}"
}

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
  'github.com/openai/openai-go/v3' \
  'ai/backend' \
  'gorm.io/cli/gorm' \
  'gorm.io/driver/sqlite' \
  'gorm.io/gorm' \
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
  'ai/backend/cmd/server' \
  'ai/backend/internal/agent' \
  'ai/backend/internal/app' \
  'ai/backend/internal/architecture' \
  'ai/backend/internal/config' \
  'ai/backend/internal/database' \
  'ai/backend/internal/database/generated' \
  'ai/backend/internal/database/queryinput' \
  'ai/backend/internal/httpapi' \
  'ai/backend/internal/platform/datastore' \
  'ai/backend/internal/platform/sqlite' \
  'ai/backend/internal/status' \
  'ai/backend/internal/toolcatalog' \
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

echo "checking GORM CLI generated code"
tmp_generated="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_generated}"
}
trap cleanup EXIT
go run gorm.io/cli/gorm@v0.2.4 gen -i internal/database/queryinput/queries.go -o "${tmp_generated}/generated" >/dev/null
if ! diff -qr internal/database/generated "${tmp_generated}/generated" >/dev/null; then
  echo "internal/database/generated is out of date; regenerate with:" >&2
  echo "go run gorm.io/cli/gorm@v0.2.4 gen -i internal/database/queryinput/queries.go -o internal/database/generated" >&2
  diff -qr internal/database/generated "${tmp_generated}/generated" >&2 || true
  exit 1
fi

echo "checking database access patterns"
while IFS= read -r file; do
  if forbidden_database_pattern "${file}"; then
    echo "${file} uses direct GORM query APIs; put SQL in internal/database/queryinput and regenerate internal/database/generated" >&2
    exit 1
  fi
done < <(find internal cmd -name '*.go' \
  -not -path 'internal/database/generated/*' \
  -not -path 'internal/platform/sqlite/*' \
  -not -name '*_test.go' \
  -print)

echo "running tests"
go test ./...

echo "running golangci-lint"
golangci-lint run ./...

echo "repo guard passed"
