#!/bin/sh
# Gateway entrypoint. Optionally seeds a fixed CL-side libp2p identity from
# /keys/$GW_KEY so the publisher can dial a known peer ID without discovery,
# then execs the gateway with an empty --config so it is configured purely from
# OPT_* environment variables.
set -eu

if [ -n "${GW_KEY:-}" ]; then
  # A configured-but-missing key would silently fall back to a fresh identity
  # and the publisher would dial a peer ID that no longer exists. Fail fast.
  [ -f "/keys/${GW_KEY}" ] || { echo "GW_KEY=${GW_KEY} set but /keys/${GW_KEY} is missing" >&2; exit 1; }
  mkdir -p "${OPT_IDENTITY_LIBP2P_DIR}"
  cp "/keys/${GW_KEY}" "${OPT_IDENTITY_LIBP2P_DIR}/p2p.key"
  echo "seeded CL libp2p identity from ${GW_KEY}"
fi

exec /usr/local/bin/optimum-gateway --config=
