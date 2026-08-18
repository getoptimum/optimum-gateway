#!/bin/sh
set -u

/rlnc-server --lanes 20 --name mump2p-protocol &
rlnc_pid=$!

/optimum-gateway "$@" &
gateway_pid=$!

cleanup() {
    kill -TERM "$gateway_pid" "$rlnc_pid" 2>/dev/null || true
    wait "$gateway_pid" "$rlnc_pid" 2>/dev/null || true
}

trap cleanup INT TERM EXIT

wait "$gateway_pid"
status=$?
exit "$status"
