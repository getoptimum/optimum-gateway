#!/usr/bin/env bash
# Discover the CDVN CL peer id and write optimum/config/app_conf.yml.
# Run from the CDVN root after the CL beacon node is up.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "${SCRIPT_DIR}/../.env" ]] && set -a && source "${SCRIPT_DIR}/../.env" && set +a

CONFIG_DIR="${SCRIPT_DIR}/config"
SAMPLE="${CONFIG_DIR}/sample.app_conf.yml"
TARGET="${CONFIG_DIR}/app_conf.yml"

CL="${CL:-cl-lighthouse}"
CL_REST_URL="${CL_REST_URL:-http://127.0.0.1:${CL_PORT_HTTP:-5052}}"

if [[ ! -f "${SAMPLE}" ]]; then
  echo "Missing ${SAMPLE}" >&2
  exit 1
fi

echo "Fetching CL peer id from ${CL_REST_URL}/eth/v1/node/identity ..."
PEER_ID="$(curl -sf "${CL_REST_URL}/eth/v1/node/identity" | jq -er '.data.peer_id')"
if [[ -z "${PEER_ID}" || "${PEER_ID}" == "null" ]]; then
  echo "Failed to read CL peer id. Is the beacon node up and ${CL_REST_URL} reachable?" >&2
  exit 1
fi

mkdir -p "${SCRIPT_DIR}/identity/libp2p" "${SCRIPT_DIR}/identity/mump2p" "${SCRIPT_DIR}/cache"

sed \
  -e "s/REPLACE_CL_HOST/${CL}/g" \
  -e "s/REPLACE_CL_PEER_ID/${PEER_ID}/g" \
  "${SAMPLE}" > "${TARGET}"

echo "Wrote ${TARGET}"
echo "  direct_cl_peers: /dns4/${CL}/tcp/9000/p2p/${PEER_ID}"
