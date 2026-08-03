#!/usr/bin/env bash
# End-to-end run of the datagram demo: build, bring up, publish, verify, stop.
#
# Everything the run produces lands in out/ (git-ignored): the captured span
# log, the span summary and the verification report.
#
# Knobs (all optional):
#   DEMO_MSG_COUNT=8            blocks to publish
#   DEMO_WARMUP=20s             publisher delay after connect, before block 1
#   DEMO_SETTLE=25              seconds to wait after the last publish
#   DEMO_DATAGRAM_ENABLE=false  negative control: run the same demo on the
#                               stream path, which must fail the transport gate
#   DEMO_SKIP_BUILD=1           reuse the existing image and binaries
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

GATEWAY_REPO="$(cd ../.. && pwd)"
BENCH_REPO="${DEMO_BENCH_REPO:-$HOME/github/optimum-bench-v2}"
IMAGE=optimum-gateway-datagram-demo:local
OUT=out
GATEWAYS=5
BASE_PORT=48131

MSG_COUNT="${DEMO_MSG_COUNT:-8}"
WARMUP="${DEMO_WARMUP:-20s}"
SETTLE="${DEMO_SETTLE:-25}"
DATAGRAM_ENABLE="${DEMO_DATAGRAM_ENABLE:-true}"

export DEMO_MSG_COUNT="$MSG_COUNT" DEMO_WARMUP="$WARMUP" DEMO_DATAGRAM_ENABLE="$DATAGRAM_ENABLE"

TAIL_PID=""

log()  { printf '\n=== %s\n' "$*"; }
fail() { printf '\nrun.sh: %s\n' "$*" >&2; exit 1; }

cleanup() {
  if [ -n "$TAIL_PID" ] && kill -0 "$TAIL_PID" 2>/dev/null; then
    kill "$TAIL_PID" 2>/dev/null || true
    wait "$TAIL_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
if [ "${DEMO_SKIP_BUILD:-0}" != "1" ]; then
  log "building binaries"
  mkdir -p bin
  # Static builds: the image is Alpine (musl) and the host toolchain is glibc,
  # so a cgo build would link against an interpreter the image does not have.
  ( cd "$GATEWAY_REPO" && CGO_ENABLED=0 go build -o "$OLDPWD/bin/optimum-gateway" ./cmd )
  # mock-bootstrap and bench-traffic come from optimum-bench-v2. Built directly
  # rather than through that repo's `make bin`, which also wants to extract a
  # gateway binary out of a published image; and CGO off, unlike its own
  # recipe, for the same musl reason as above.
  [ -d "$BENCH_REPO" ] || fail "optimum-bench-v2 not found at $BENCH_REPO (set DEMO_BENCH_REPO)"
  ( cd "$BENCH_REPO" \
      && CGO_ENABLED=0 go build -o "$OLDPWD/bin/mock-bootstrap" ./mocks/bootstrap \
      && CGO_ENABLED=0 go build -o "$OLDPWD/bin/bench-traffic" ./tools/bench-traffic )

  log "building image $IMAGE"
  docker build -t "$IMAGE" -f Dockerfile . >/dev/null
fi

# ---------------------------------------------------------------------------
# Bring up, in stages
# ---------------------------------------------------------------------------
mkdir -p "$OUT"
log "tearing down any previous run"
docker compose down -v --remove-orphans >/dev/null 2>&1 || true

log "starting the control plane (bootstrap, config proxy, collector)"
docker compose up -d bootstrap configproxy otel-collector

# Capture the collector's stdout from before any traffic exists. The debug
# exporter prints spans through the collector's logger, so this file is the only
# record of them; `logs -f` drains it continuously into out/.
log "capturing spans to $OUT/otel-collector.log"
: > "$OUT/otel-collector.log"
docker compose logs -f --no-log-prefix otel-collector >> "$OUT/otel-collector.log" 2>&1 &
TAIL_PID=$!

# Gateways start one at a time and each is waited for. Peer discovery is
# strictly ordered: a gateway registers itself and then asks the bootstrap for
# the peers registered before it, so starting gateway N only once gateway N-1 is
# serving HTTP is what makes the resulting mesh deterministic rather than
# whatever the startup race produced.
for i in $(seq 1 $GATEWAYS); do
  port=$((BASE_PORT + i - 1))
  log "starting gateway$i"
  docker compose up -d "gateway$i"
  ready=0
  for _ in $(seq 1 90); do
    if curl -sf -m 2 "http://127.0.0.1:$port/api/v1/self_info" >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  [ "$ready" = 1 ] || { docker compose logs "gateway$i" | tail -30; fail "gateway$i did not come up"; }
  sleep 0.3
done

# ---------------------------------------------------------------------------
# Wait for the mesh, then for the datagram paths
# ---------------------------------------------------------------------------
peers_wanted=$((GATEWAYS - 1))

log "waiting for a full mesh ($peers_wanted peers each)"
for _ in $(seq 1 120); do
  ok=1
  for i in $(seq 1 $GATEWAYS); do
    port=$((BASE_PORT + i - 1))
    n=$(curl -sf -m 2 "http://127.0.0.1:$port/api/v1/self_info" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["mump2p"]["total_peers"])' 2>/dev/null || echo 0)
    [ "$n" = "$peers_wanted" ] || { ok=0; break; }
  done
  [ "$ok" = 1 ] && break
  sleep 2
done
[ "${ok:-0}" = 1 ] || fail "the mump2p mesh never reached $peers_wanted peers on every gateway"

if [ "$DATAGRAM_ENABLE" = "true" ]; then
  log "waiting for every datagram path to be confirmed"
  for _ in $(seq 1 60); do
    ok=1
    for i in $(seq 1 $GATEWAYS); do
      port=$((BASE_PORT + i - 1))
      c=$(curl -sf -m 2 "http://127.0.0.1:$port/api/v1/self_info" \
          | python3 -c 'import json,sys; d=json.load(sys.stdin)["datagram"]; print(1 if d["peers_total"]>0 and d["paths_confirmed"]==d["peers_total"] else 0)' 2>/dev/null || echo 0)
      [ "$c" = "1" ] || { ok=0; break; }
    done
    [ "$ok" = 1 ] && break
    sleep 2
  done
  [ "${ok:-0}" = 1 ] || fail "not every gateway confirmed a datagram path to every peer"
fi

# ---------------------------------------------------------------------------
# Publish
# ---------------------------------------------------------------------------
log "publishing $MSG_COUNT beacon blocks through gateway1's CL ingress (warmup $WARMUP, one per 12s slot)"
docker compose up -d publisher
# The publisher exits on its own once it has sent its last block.
publish_deadline=$(( $(date +%s) + 60 + MSG_COUNT * 15 ))
while docker compose ps --status running --services 2>/dev/null | grep -qx publisher; do
  [ "$(date +%s)" -lt "$publish_deadline" ] || fail "the publisher did not finish in time"
  sleep 2
done
docker compose logs publisher 2>&1 | tail -5

log "settling for ${SETTLE}s (last decodes, then the span batch export)"
sleep "$SETTLE"
sync

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
log "verifying"
set +e
python3 ./verify.py \
  --gateways "$GATEWAYS" --base-port "$BASE_PORT" \
  --expect-messages "$MSG_COUNT" \
  --spans "$OUT/otel-collector.log" \
  | tee "$OUT/verify.txt"
rc=${PIPESTATUS[0]}
set -e

# ---------------------------------------------------------------------------
# Shut down cleanly and archive
# ---------------------------------------------------------------------------
# SIGTERM, never a kill: the gateways' span processor flushes for 5s on
# shutdown, and killing them drops the tail of the trace.
log "stopping (SIGTERM, so the span flush runs)"
docker compose stop
sleep 8
cleanup; TAIL_PID=""

python3 ./parse_v2_1.py "$OUT/otel-collector.log" > "$OUT/spans.txt" 2>&1 || true

log "artifacts"
ls -la "$OUT"
printf '\n'
if [ "$rc" -eq 0 ]; then
  echo "run.sh: PASS"
else
  echo "run.sh: FAIL (see $OUT/verify.txt)"
fi
exit "$rc"
