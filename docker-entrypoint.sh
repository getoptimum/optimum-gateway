#!/bin/sh
set -u

/rlnc-server --lanes 20 --name mump2p-protocol &
rlnc_pid=$!

lane_ready() {
	for f in /dev/shm/go_shm_rlnc_semaphore_mump2p-protocol_lane*; do
		[ -e "$f" ] && return 0
	done
	return 1
}

i=0
until lane_ready; do
	if [ "$i" -ge 30 ] || ! kill -0 "$rlnc_pid" 2>/dev/null; then
		echo "rlnc-server did not create a shared-memory lane" >&2
		kill -TERM "$rlnc_pid" 2>/dev/null || true
		exit 1
	fi
	i=$((i + 1))
	sleep 1
done

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
