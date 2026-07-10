#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-8081}"
API_LOG="${API_LOG:-/tmp/swap-httpapi.log}"

cleanup() {
  if [[ -n "${API_PID:-}" ]]; then
    kill "${API_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

cd "$(dirname "$0")/.."

ENV_FILE=".env"
ENV_PATH="$(pwd)/${ENV_FILE}"
ENV_LOADED=0
ENV_OVERRIDDEN=0
ENV_FOUND=0
ENV_LOADED_KEYS=""
ENV_OVERRIDDEN_KEYS=""

# Load local environment file if present.
# Command-line environment variables take precedence over .env values.
if [[ -f "${ENV_FILE}" ]]; then
  ENV_FOUND=1
  while IFS='=' read -r key value; do
    if [[ -z "${key}" ]] || [[ "${key}" == \#* ]]; then
      continue
    fi
    if [[ -z "${!key+x}" ]]; then
      export "${key}=${value}"
      ENV_LOADED=$((ENV_LOADED + 1))
      if [[ -n "${ENV_LOADED_KEYS}" ]]; then
        ENV_LOADED_KEYS+=" "
      fi
      ENV_LOADED_KEYS+="${key}"
    else
      ENV_OVERRIDDEN=$((ENV_OVERRIDDEN + 1))
      if [[ -n "${ENV_OVERRIDDEN_KEYS}" ]]; then
        ENV_OVERRIDDEN_KEYS+=" "
      fi
      ENV_OVERRIDDEN_KEYS+="${key}"
    fi
  done < "${ENV_FILE}"
fi

FORCE_KILL_PORT="${SWAP_FORCE_KILL_PORT:-0}"
SELF_CHECK="${SWAP_SELF_CHECK:-1}"
SELF_CHECK_FROM="${SWAP_SELF_CHECK_FROM:-USDC}"
SELF_CHECK_TO="${SWAP_SELF_CHECK_TO:-ETH}"
SELF_CHECK_AMOUNT="${SWAP_SELF_CHECK_AMOUNT:-1}"
SELF_CHECK_CHAIN_ID="${SWAP_SELF_CHECK_CHAIN_ID:-${SWAP_0X_CHAIN_ID:-1}}"
SELF_CHECK_MIN_PROVIDERS="${SWAP_SELF_CHECK_MIN_PROVIDERS:-1}"
SKIP_TUI="${SWAP_SKIP_TUI:-0}"

as_bool_01() {
  local value="${1:-}"
  local default="${2:-0}"
  local lower
  lower="$(printf '%s' "${value}" | tr '[:upper:]' '[:lower:]')"
  case "${lower}" in
    1|true|yes|on)
      echo "1"
      ;;
    0|false|no|off|"")
      echo "0"
      ;;
    *)
      echo "${default}"
      ;;
  esac
}

TUI_DEBUG="$(as_bool_01 "${SWAP_DEBUG:-0}" 0)"
TUI_NO_LOGS="$(as_bool_01 "${SWAP_NO_LOGS:-0}" 0)"
TUI_LEGAL="$(as_bool_01 "${SWAP_LEGAL:-0}" 0)"
TUI_WARNINGS="$(as_bool_01 "${SWAP_UI_SHOW_WARNING_DETAILS:-1}" 1)"
ENV_DEBUG="$(as_bool_01 "${SWAP_ENV_DEBUG:-0}" 0)"

provider_list="${SWAP_QUOTE_PROVIDER:-0x,paraswap}"
provider_lc="$(printf '%s' "$provider_list" | tr '[:upper:]' '[:lower:]')"

echo "Config: provider=${provider_list} chain=${SWAP_0X_CHAIN_ID:-137} force_kill_port=${FORCE_KILL_PORT} self_check=${SELF_CHECK}"
if [[ "${ENV_FOUND}" == "1" ]]; then
  echo ".env source: ${ENV_PATH} loaded=${ENV_LOADED} overridden=${ENV_OVERRIDDEN}"
  if [[ "${ENV_DEBUG}" == "1" ]]; then
    echo ".env loaded keys: ${ENV_LOADED_KEYS:-none}"
    echo ".env overridden keys: ${ENV_OVERRIDDEN_KEYS:-none}"
  fi
else
  echo ".env source: not found at ${ENV_PATH}"
fi
echo "TUI env flags: debug=${TUI_DEBUG} no_logs=${TUI_NO_LOGS} legal=${TUI_LEGAL} warnings_detail=${TUI_WARNINGS}"

# Fail fast when 0x is enabled without an API key.
if [[ ",${provider_lc// /}," == *",0x,"* ]] && [[ -z "${SWAP_0X_API_KEY:-}" ]]; then
  echo "error: SWAP_0X_API_KEY is required when SWAP_QUOTE_PROVIDER includes 0x" >&2
  echo "hint: export SWAP_0X_API_KEY=... or set it in .env" >&2
  exit 1
fi

# Avoid reusing stale httpapi processes with old env/config.
existing_pid="$(lsof -tiTCP:${PORT} -sTCP:LISTEN 2>/dev/null || true)"
if [[ -n "${existing_pid}" ]]; then
  if [[ "${FORCE_KILL_PORT}" == "1" ]]; then
    echo "Port ${PORT} is in use by PID ${existing_pid}; stopping it (SWAP_FORCE_KILL_PORT=1) ..."
    kill "${existing_pid}" >/dev/null 2>&1 || true
    sleep 0.2
    if lsof -tiTCP:${PORT} -sTCP:LISTEN >/dev/null 2>&1; then
      echo "error: failed to free port ${PORT} after stopping PID ${existing_pid}" >&2
      exit 1
    fi
  else
    echo "error: port ${PORT} is already in use by PID ${existing_pid}" >&2
    echo "hint: run 'kill ${existing_pid}' and retry, or set SWAP_FORCE_KILL_PORT=1" >&2
    exit 1
  fi
fi

echo "Starting local httpapi on :${PORT} ..."
SWAP_QUOTE_PROVIDER="${provider_list}" \
SWAP_0X_API_KEY="${SWAP_0X_API_KEY:-}" \
SWAP_0X_CHAIN_ID="${SWAP_0X_CHAIN_ID:-137}" \
SWAP_1INCH_CHAIN_ID="${SWAP_1INCH_CHAIN_ID:-${SWAP_0X_CHAIN_ID:-137}}" \
SWAP_0X_TAKER="${SWAP_0X_TAKER:-0x0000000000000000000000000000000000010000}" \
SWAP_COINS_SOURCE="${SWAP_COINS_SOURCE:-coingecko}" \
SWAP_COINS_LIMIT="${SWAP_COINS_LIMIT:-120}" \
SWAP_COINS_CACHE_TTL="${SWAP_COINS_CACHE_TTL:-10m}" \
CGO_ENABLED=0 go run ./cmd/httpapi >"${API_LOG}" 2>&1 &
API_PID=$!
echo "httpapi started with PID ${API_PID}; waiting for healthz ..."

for _ in $(seq 1 40); do
  if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done

if ! curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
  echo "httpapi failed to start; see ${API_LOG}" >&2
  tail -n 60 "${API_LOG}" || true
  exit 1
fi

echo "httpapi is healthy on :${PORT} (PID ${API_PID})"

if [[ "${SELF_CHECK}" == "1" ]]; then
  echo "Running quote self-check (${SELF_CHECK_FROM}->${SELF_CHECK_TO}, amount=${SELF_CHECK_AMOUNT}, chain=${SELF_CHECK_CHAIN_ID}, min_providers=${SELF_CHECK_MIN_PROVIDERS}) ..."
  check_payload="$(cat <<EOF
{"ticker_from":"${SELF_CHECK_FROM}","ticker_to":"${SELF_CHECK_TO}","amount_from":"${SELF_CHECK_AMOUNT}","chain_id":"${SELF_CHECK_CHAIN_ID}"}
EOF
)"
  check_resp="$(curl -sS --max-time 15 -X POST "http://127.0.0.1:${PORT}/v1/swaprate" -H "Content-Type: application/json" -d "${check_payload}" || true)"
  if [[ -z "${check_resp}" ]] || [[ "${check_resp}" == failed* ]] || [[ "${check_resp}" == *'"quotes":[]'* ]]; then
    echo "error: quote self-check failed; response: ${check_resp}" >&2
    echo "hint: verify SWAP_0X_API_KEY and provider reachability, then retry" >&2
    tail -n 60 "${API_LOG}" || true
    exit 1
  fi

  provider_lines="$(printf '%s' "${check_resp}" | grep -o '"provider":"[^"]*"' | sed -E 's/"provider":"([^"]*)"/\1/' || true)"
  provider_count="$(printf '%s\n' "${provider_lines}" | sed '/^$/d' | sort -u | wc -l | tr -d ' ')"
  if [[ -z "${provider_count}" ]]; then
    provider_count=0
  fi

  if [[ "${provider_count}" -lt "${SELF_CHECK_MIN_PROVIDERS}" ]]; then
    echo "error: quote self-check provider diversity check failed; got ${provider_count}, need at least ${SELF_CHECK_MIN_PROVIDERS}" >&2
    echo "hint: check SWAP_QUOTE_PROVIDER and upstream provider reachability" >&2
    echo "response: ${check_resp}" >&2
    tail -n 60 "${API_LOG}" || true
    exit 1
  fi

  provider_summary="$(printf '%s\n' "${provider_lines}" | sed '/^$/d' | sort | uniq -c | awk '{printf "%s%s:%s", sep, $2, $1; sep=", "}')"
  if [[ -z "${provider_summary}" ]]; then
    provider_summary="none"
  fi

  echo "quote self-check passed (providers=${provider_summary}; unique=${provider_count})"
fi

echo "httpapi is ready"
if [[ "${SKIP_TUI}" == "1" ]]; then
  echo "SWAP_SKIP_TUI=1 set; keeping httpapi running and skipping TUI launch"
  wait "${API_PID}"
  api_exit_code=$?
  if [[ "${api_exit_code}" -ne 0 ]]; then
    echo "error: httpapi exited with code ${api_exit_code}; see ${API_LOG}" >&2
    tail -n 60 "${API_LOG}" || true
    exit "${api_exit_code}"
  fi
  exit 0
fi

echo "Building swap TUI binary ..."
CGO_ENABLED=0 go build -o /tmp/swap-tui ./cmd/swap

echo "Launching swap TUI (SWAP_API_URL=http://127.0.0.1:${PORT}) ..."
SWAP_API_URL="http://127.0.0.1:${PORT}" /tmp/swap-tui
api_exit_code=$?
echo "swap TUI exited with code ${api_exit_code}"
exit "${api_exit_code}"
