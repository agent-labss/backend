#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

database_path="${1:-sqlite.db}"
log_file="$(mktemp)"
server_pid=""

cleanup() {
  if [[ -n "${server_pid}" ]] && kill -0 "${server_pid}" >/dev/null 2>&1; then
    kill -TERM "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" >/dev/null 2>&1 || true
  fi
  rm -f "${log_file}"
}
trap cleanup EXIT

DATABASE_URL="${database_path}" HTTP_ADDR=127.0.0.1:0 go run ./cmd/server >"${log_file}" 2>&1 &
server_pid="$!"

for _ in $(seq 1 60); do
  if [[ -f "${database_path}" ]]; then
    kill -TERM "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" >/dev/null 2>&1 || true
    server_pid=""
    echo "generated ${database_path}"
    exit 0
  fi

  if ! kill -0 "${server_pid}" >/dev/null 2>&1; then
    cat "${log_file}" >&2
    echo "server exited before creating ${database_path}" >&2
    exit 1
  fi

  sleep 0.25
done

cat "${log_file}" >&2
echo "timed out waiting for ${database_path}" >&2
exit 1
