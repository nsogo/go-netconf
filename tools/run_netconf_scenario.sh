#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# run_netconf_scenario.sh
#
# Usage:
#   ./tools/run_netconf_scenario.sh [options]
#
# Options:
#   --mode     normal | timeout   (default: normal)
#   --interval loop interval in seconds (default: 60)
#   --count    number of iterations, 0 = infinite (default: 3)
#   --timeout  NETCONF timeout in seconds applied to DialSSHTimeout (default: 10)
#              Matches the value hardcoded in typical collector_agent deployments.
#              Change this to observe how different timeout values affect behavior.
#   --delay    mock response delay in seconds, timeout mode only (default: 15)
#   --host     NETCONF mock host (default: localhost)
#   --port     NETCONF mock port (default: 830)
#   --http-port mock HTTP control port (default: 8088)
#   --user     SSH username (default: admin)
#   --password SSH password (default: admin)
#   --rpc      NETCONF RPC XML to execute (default: <get/>)
#              Example: --rpc '<get-vrrp-information><summary/></get-vrrp-information>'
#   --no-build skip docker-compose build
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CLIENT_BIN="${REPO_ROOT}/netconf-client"

MODE="normal"
INTERVAL=60
COUNT=3
TIMEOUT=10
MOCK_DELAY=15
HOST="localhost"
PORT=830
HTTP_PORT=8088
USER="admin"
PASSWORD="admin"
NO_BUILD=false
RPC="<get-vrrp-information><summary/></get-vrrp-information>"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [$1] ${*:2}"
}

usage() {
    grep '^#' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)       MODE="$2";             shift 2 ;;
        --interval)   INTERVAL="$2";         shift 2 ;;
        --count)      COUNT="$2";            shift 2 ;;
        --timeout)    TIMEOUT="$2";           shift 2 ;;
        --delay)      MOCK_DELAY="$2";       shift 2 ;;
        --host)       HOST="$2";             shift 2 ;;
        --port)       PORT="$2";             shift 2 ;;
        --http-port)  HTTP_PORT="$2";        shift 2 ;;
        --user)       USER="$2";             shift 2 ;;
        --password)   PASSWORD="$2";         shift 2 ;;
        --rpc)        RPC="$2";              shift 2 ;;
        --no-build)   NO_BUILD=true;         shift ;;
        -h|--help)    usage ;;
        *) log "ERROR" "Unknown option: $1"; exit 1 ;;
    esac
done

if [[ "$MODE" != "normal" && "$MODE" != "timeout" ]]; then
    log "ERROR" "--mode must be 'normal' or 'timeout'"
    exit 1
fi

# ---------------------------------------------------------------------------
# Build netconf-client if not present
# ---------------------------------------------------------------------------
if [[ ! -x "${CLIENT_BIN}" ]]; then
    log "INFO " "Building netconf-client..."
    cd "${REPO_ROOT}"
    go build -o netconf-client ./cmd/netconf-client/
    log "INFO " "Build complete: ${CLIENT_BIN}"
fi

# ---------------------------------------------------------------------------
# Start netconf-mock via docker-compose
# ---------------------------------------------------------------------------
log "INFO " "Starting netconf-mock..."
cd "${REPO_ROOT}"
if [[ "${NO_BUILD}" == "false" ]]; then
    docker compose build netconf-mock
fi
docker compose up -d netconf-mock

# Wait for mock to become ready
MOCK_URL="http://${HOST}:${HTTP_PORT}"
log "INFO " "Waiting for netconf-mock to be ready at ${MOCK_URL} ..."
for i in $(seq 1 20); do
    if curl -sf "${MOCK_URL}/" > /dev/null 2>&1; then
        log "INFO " "netconf-mock is ready"
        break
    fi
    if [[ $i -eq 20 ]]; then
        log "ERROR" "netconf-mock did not become ready in time"
        docker compose logs netconf-mock
        exit 1
    fi
    sleep 1
done

# ---------------------------------------------------------------------------
# Configure mock for timeout mode
# ---------------------------------------------------------------------------
if [[ "${MODE}" == "timeout" ]]; then
    log "INFO " "Enabling delays (mock delay=${MOCK_DELAY}s, NETCONF timeout=${TIMEOUT}s)"
    curl -sf -X POST "${MOCK_URL}/set_use_delays" > /dev/null
    curl -sf -X POST "${MOCK_URL}/delays_range" \
        -H "Content-Type: application/json" \
        -d "{\"delay\": ${MOCK_DELAY}}" > /dev/null
    log "INFO " "Delay configured: mock will wait ${MOCK_DELAY}s before responding"
else
    # Ensure delays are off for normal mode
    curl -sf -X POST "${MOCK_URL}/set_no_delays" > /dev/null
fi

# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------
SUCCESS=0
FAILED=0
ITERATION=0

if [[ "${COUNT}" -eq 0 ]]; then
    COUNT_LABEL="infinite"
else
    COUNT_LABEL="${COUNT}"
fi

log "INFO " "Starting netconf scenario (mode=${MODE}, count=${COUNT_LABEL}, interval=${INTERVAL}s, timeout=${TIMEOUT}s, rpc=${RPC})"

while true; do
    ITERATION=$((ITERATION + 1))

    if [[ "${COUNT}" -gt 0 && "${ITERATION}" -gt "${COUNT}" ]]; then
        break
    fi

    ITER_LABEL="${ITERATION}"
    if [[ "${COUNT}" -gt 0 ]]; then
        ITER_LABEL="${ITERATION}/${COUNT}"
    fi

    log "INFO " "--- Iteration ${ITER_LABEL} ---"

    TIMEOUT_FLAG="${TIMEOUT}s"

    set +e
    NETCONF_DEBUG=1 "${CLIENT_BIN}" \
        --host "${HOST}" \
        --port "${PORT}" \
        --user "${USER}" \
        --password "${PASSWORD}" \
        --timeout "${TIMEOUT_FLAG}" \
        --rpc "${RPC}" \
        --debug
    EXIT_CODE=$?
    set -e

    if [[ ${EXIT_CODE} -eq 0 ]]; then
        SUCCESS=$((SUCCESS + 1))
        log "INFO " "Iteration ${ITER_LABEL} SUCCESS"
    else
        FAILED=$((FAILED + 1))
        log "ERROR" "Iteration ${ITER_LABEL} FAILED (exit=${EXIT_CODE})"
    fi

    if [[ "${COUNT}" -gt 0 && "${ITERATION}" -ge "${COUNT}" ]]; then
        break
    fi

    log "INFO " "Waiting ${INTERVAL}s before next iteration..."
    sleep "${INTERVAL}"
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
TOTAL=$((SUCCESS + FAILED))
log "INFO " "============================================"
log "INFO " "Scenario finished. Success: ${SUCCESS} / Failed: ${FAILED} / Total: ${TOTAL}"
log "INFO " "============================================"

if [[ ${FAILED} -gt 0 ]]; then
    exit 1
fi
exit 0
