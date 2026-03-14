#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLES_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${RESULTS_DIR:-${EXAMPLES_DIR}/.wrk-results/$(date +%Y%m%d-%H%M%S)}"
BIN_DIR="${BIN_DIR:-${EXAMPLES_DIR}/.wrk-bin}"
HOST="${HOST:-localhost}"
PORT="${PORT:-8080}"
ADDR="${HOST}:${PORT}"
PATH_UNDER_TEST="${PATH_UNDER_TEST:-/}"
THREADS="${THREADS:-6}"
CONNECTIONS="${CONNECTIONS:-300}"
DURATION="${DURATION:-30s}"
WARMUPS="${WARMUPS:-0}"
RUNS="${RUNS:-3}"
SERVER_LIFETIME="${SERVER_LIFETIME:-per-kind}"
COOLDOWN_SECS="${COOLDOWN_SECS:-1}"
WITH_LATENCY="${WITH_LATENCY:-0}"
STD_ENV_ADDR="${STD_ENV_ADDR:-:${PORT}}"
HOP_ENV_ADDR="${HOP_ENV_ADDR:-:${PORT}}"

mkdir -p "${RESULTS_DIR}" "${BIN_DIR}"

if ! command -v wrk >/dev/null 2>&1; then
  printf 'wrk is required but not installed.\n' >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  printf 'python3 is required but not installed.\n' >&2
  exit 1
fi

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  SERVER_PID=""
}
trap cleanup EXIT

build_binaries() {
  printf 'Building example servers...\n'
  (cd "${EXAMPLES_DIR}" && go build -o "${BIN_DIR}/stdhttp_example" ./cmd/stdhttp_example)
  (cd "${EXAMPLES_DIR}" && go build -o "${BIN_DIR}/netpoll_example" ./cmd/netpoll_example)
}

wait_for_server() {
  local url="$1"
  python3 - "$url" <<'PY'
import sys, time, urllib.request
url = sys.argv[1]
deadline = time.time() + 10
while time.time() < deadline:
    try:
        with urllib.request.urlopen(url, timeout=1) as response:
            response.read(1)
            sys.exit(0)
    except Exception:
        time.sleep(0.1)
print(f"server did not become ready: {url}", file=sys.stderr)
sys.exit(1)
PY
}

start_server() {
  local kind="$1"
  cleanup
  local bin env_addr log_file
  case "$kind" in
    std)
      bin="${BIN_DIR}/stdhttp_example"
      env_addr="${STD_ENV_ADDR}"
      ;;
    hop)
      bin="${BIN_DIR}/netpoll_example"
      env_addr="${HOP_ENV_ADDR}"
      ;;
    *)
      printf 'unknown server kind: %s\n' "$kind" >&2
      exit 1
      ;;
  esac
  log_file="${RESULTS_DIR}/${kind}.server.log"
  printf 'Starting %s server on %s...\n' "$kind" "$env_addr"
  ADDR="$env_addr" "$bin" >"$log_file" 2>&1 &
  SERVER_PID=$!
  wait_for_server "http://${ADDR}${PATH_UNDER_TEST}"
}

run_wrk() {
  local kind="$1"
  local phase="$2"
  local index="$3"
  local out_file="${RESULTS_DIR}/${kind}.${phase}.${index}.txt"
  local -a wrk_args
  wrk_args=( -t"${THREADS}" -c"${CONNECTIONS}" -d"${DURATION}" )
  if [[ "${WITH_LATENCY}" == "1" ]]; then
    wrk_args+=( --latency )
  fi
  printf 'Running wrk: kind=%s phase=%s index=%s\n' "$kind" "$phase" "$index"
  wrk "${wrk_args[@]}" "http://${ADDR}${PATH_UNDER_TEST}" | tee "$out_file"
}

append_metric() {
  local kind="$1"
  local phase="$2"
  local index="$3"
  local file="$4"
  python3 - "$kind" "$phase" "$index" "$file" >>"${RESULTS_DIR}/summary.tsv" <<'PY'
import re, sys
kind, phase, index, path = sys.argv[1:5]
text = open(path, 'r', encoding='utf-8').read()
patterns = {
    'requests_per_sec': r'Requests/sec:\s+([0-9.]+)',
    'transfer_per_sec': r'Transfer/sec:\s+([0-9.]+)(\w+)',
    'latency_avg': r'Latency\s+([0-9.]+)(\w+)',
    'latency_stdev': r'Latency\s+[0-9.]+\w+\s+([0-9.]+)(\w+)',
    'latency_max': r'Latency\s+[0-9.]+\w+\s+[0-9.]+\w+\s+([0-9.]+)(\w+)',
    'req_per_sec_avg': r'Req/Sec\s+([0-9.]+)([kM]?)',
    'req_per_sec_stdev': r'Req/Sec\s+[0-9.]+[kM]?\s+([0-9.]+)([kM]?)',
    'req_per_sec_max': r'Req/Sec\s+[0-9.]+[kM]?\s+[0-9.]+[kM]?\s+([0-9.]+)([kM]?)',
}

def grab(name):
    match = re.search(patterns[name], text)
    if not match:
        return ''
    if len(match.groups()) == 2:
        return ''.join(match.groups())
    return match.group(1)

socket_errors = ''
match = re.search(r'Socket errors:\s+connect\s+(\d+),\s+read\s+(\d+),\s+write\s+(\d+),\s+timeout\s+(\d+)', text)
if match:
    socket_errors = '/'.join(match.groups())
print('\t'.join([
    kind,
    phase,
    index,
    grab('requests_per_sec'),
    grab('transfer_per_sec'),
    grab('latency_avg'),
    grab('latency_stdev'),
    grab('latency_max'),
    grab('req_per_sec_avg'),
    grab('req_per_sec_stdev'),
    grab('req_per_sec_max'),
    socket_errors,
]))
PY
}

run_series() {
  local kind="$1"
  local i file
  if [[ "${SERVER_LIFETIME}" != "per-kind" && "${SERVER_LIFETIME}" != "per-run" ]]; then
    printf 'SERVER_LIFETIME must be per-kind or per-run, got %s\n' "${SERVER_LIFETIME}" >&2
    exit 1
  fi
  start_server "$kind"
  for ((i=1; i<=WARMUPS; i++)); do
    run_wrk "$kind" "warmup" "$i" >/dev/null
  done
  for ((i=1; i<=RUNS; i++)); do
    if [[ "$i" -gt 1 && "${SERVER_LIFETIME}" == "per-run" ]]; then
      cleanup
      sleep "${COOLDOWN_SECS}"
      start_server "$kind"
    fi
    file="${RESULTS_DIR}/${kind}.run.${i}.txt"
    run_wrk "$kind" "run" "$i"
    append_metric "$kind" "run" "$i" "$file"
    if [[ "${SERVER_LIFETIME}" == "per-run" ]]; then
      cleanup
      sleep "${COOLDOWN_SECS}"
    fi
  done
  cleanup
  sleep "${COOLDOWN_SECS}"
}

write_summary_header() {
  printf 'kind\tphase\tindex\trequests_per_sec\ttransfer_per_sec\tlatency_avg\tlatency_stdev\tlatency_max\treq_per_sec_avg\treq_per_sec_stdev\treq_per_sec_max\tsocket_errors\n' >"${RESULTS_DIR}/summary.tsv"
}

print_summary() {
  python3 - "${RESULTS_DIR}/summary.tsv" <<'PY'
import csv, statistics, sys
path = sys.argv[1]
rows = []
with open(path, newline='', encoding='utf-8') as f:
    reader = csv.DictReader(f, delimiter='\t')
    rows = [row for row in reader if row['phase'] == 'run']

by_kind = {}
for row in rows:
    by_kind.setdefault(row['kind'], []).append(float(row['requests_per_sec']))

print('\nSummary (Requests/sec median, min, max):')
for kind, values in sorted(by_kind.items()):
    print(f"- {kind}: median={statistics.median(values):.2f}, min={min(values):.2f}, max={max(values):.2f}, runs={len(values)}")
PY
}

print_config() {
  printf 'Configuration:\n'
  printf '  HOST=%s\n' "${HOST}"
  printf '  PORT=%s\n' "${PORT}"
  printf '  PATH_UNDER_TEST=%s\n' "${PATH_UNDER_TEST}"
  printf '  THREADS=%s\n' "${THREADS}"
  printf '  CONNECTIONS=%s\n' "${CONNECTIONS}"
  printf '  DURATION=%s\n' "${DURATION}"
  printf '  WARMUPS=%s\n' "${WARMUPS}"
  printf '  RUNS=%s\n' "${RUNS}"
  printf '  SERVER_LIFETIME=%s\n' "${SERVER_LIFETIME}"
  printf '  WITH_LATENCY=%s\n' "${WITH_LATENCY}"
  printf '  RESULTS_DIR=%s\n\n' "${RESULTS_DIR}"
}

build_binaries
write_summary_header
print_config
run_series std
run_series hop
print_summary

printf '\nRaw results saved under %s\n' "${RESULTS_DIR}"
