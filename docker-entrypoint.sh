#!/bin/sh
set -u

/rlnc-server --lanes 20 --name mump2p-protocol &
rlnc_pid=$!

i=0
while [ "$i" -lt 30 ]; do
	if ! kill -0 "$rlnc_pid" 2>/dev/null; then
		wait "$rlnc_pid"
		exit $?
	fi
	set -- /dev/shm/go_shm_rlnc_semaphore_mump2p-protocol_lane*
	if [ -e "$1" ]; then
		break
	fi
	i=$((i + 1))
	sleep 1
done
set -- /dev/shm/go_shm_rlnc_semaphore_mump2p-protocol_lane*
if [ ! -e "$1" ]; then
	kill -TERM "$rlnc_pid" 2>/dev/null || true
	exit 1
fi

/optimum-gateway "$@" &
gateway_pid=$!

cleanup() {
	kill -TERM "$gateway_pid" "$rlnc_pid" 2>/dev/null || true
	wait "$gateway_pid" "$rlnc_pid" 2>/dev/null || true
}

trap cleanup INT TERM EXIT

while kill -0 "$gateway_pid" 2>/dev/null && kill -0 "$rlnc_pid" 2>/dev/null; do
	sleep 1
done
if kill -0 "$gateway_pid" 2>/dev/null; then
	wait "$rlnc_pid"
else
	wait "$gateway_pid"
fi
status=$?
exit "$status"
