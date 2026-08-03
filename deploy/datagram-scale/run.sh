#!/usr/bin/env bash
# One mid-scale run: generate the stack, build, bring up, publish, report, stop.
#
# Unlike the five-gateway stack, this one is not a pass/fail gate. It publishes,
# measures how far the generations got and prints the numbers. A run that
# delivers nothing is a successful run.
#
# Usage:
#   run.sh [--gateways 12] [--forward-threshold 0.75] [--mesh-degree 6]
#          [--shard-factor 16] [--publisher-multiplier 2.5] [--lanes 20]
#          [--messages 8] [--warmup 20s] [--settle 25] [--base-port 48151]
#          [--tag NAME] [--skip-build] [--keep-up] [--no-datagram]
#
# Every flag also has an environment variable (SCALE_GATEWAYS, SCALE_FORWARD_THRESHOLD,
# SCALE_MESH_DEGREE, SCALE_SHARD_FACTOR, SCALE_PUBLISHER_MULTIPLIER, SCALE_LANES,
# SCALE_MSG_COUNT, SCALE_WARMUP, SCALE_SETTLE, SCALE_BASE_PORT, SCALE_SKIP_BUILD,
# SCALE_BENCH_REPO), so sweep.sh can drive it either way.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

GATEWAY_REPO="$(cd ../.. && pwd)"
BENCH_REPO="${SCALE_BENCH_REPO:-$HOME/github/optimum-bench-v2}"
IMAGE=optimum-gateway-datagram-scale:local
COMPOSE="docker compose -f stack.yml"

GATEWAYS="${SCALE_GATEWAYS:-12}"
FORWARD_THRESHOLD="${SCALE_FORWARD_THRESHOLD:-0.75}"
MESH_DEGREE="${SCALE_MESH_DEGREE:-6}"
SHARD_FACTOR="${SCALE_SHARD_FACTOR:-16}"
PUBLISHER_MULTIPLIER="${SCALE_PUBLISHER_MULTIPLIER:-2.5}"
LANES="${SCALE_LANES:-20}"
MSG_COUNT="${SCALE_MSG_COUNT:-8}"
WARMUP="${SCALE_WARMUP:-20s}"
SETTLE="${SCALE_SETTLE:-25}"
BASE_PORT="${SCALE_BASE_PORT:-48151}"
SKIP_BUILD="${SCALE_SKIP_BUILD:-0}"
DATAGRAM_ENABLE="${SCALE_DATAGRAM_ENABLE:-true}"
TAG=""
KEEP_UP=0

while [ $# -gt 0 ]; do
  case "$1" in
    --gateways)              GATEWAYS="$2"; shift 2 ;;
    --forward-threshold|-f)  FORWARD_THRESHOLD="$2"; shift 2 ;;
    --mesh-degree)           MESH_DEGREE="$2"; shift 2 ;;
    --shard-factor|-k)       SHARD_FACTOR="$2"; shift 2 ;;
    --publisher-multiplier)  PUBLISHER_MULTIPLIER="$2"; shift 2 ;;
    --lanes)                 LANES="$2"; shift 2 ;;
    --messages)              MSG_COUNT="$2"; shift 2 ;;
    --warmup)                WARMUP="$2"; shift 2 ;;
    --settle)                SETTLE="$2"; shift 2 ;;
    --base-port)             BASE_PORT="$2"; shift 2 ;;
    --tag)                   TAG="$2"; shift 2 ;;
    --skip-build)            SKIP_BUILD=1; shift ;;
    --keep-up)               KEEP_UP=1; shift ;;
    --no-datagram)           DATAGRAM_ENABLE=false; shift ;;
    -h|--help)               sed -n '2,20p' "$0"; exit 0 ;;
    *) printf 'run.sh: unknown argument %s\n' "$1" >&2; exit 2 ;;
  esac
done

OUT="out/${TAG:-p${GATEWAYS}-f${FORWARD_THRESHOLD}}"

export SCALE_MSG_COUNT="$MSG_COUNT" SCALE_WARMUP="$WARMUP" SCALE_DATAGRAM_ENABLE="$DATAGRAM_ENABLE"

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
# Generate
# ---------------------------------------------------------------------------
log "generating the stack"
python3 ./gen-stack.py \
  --gateways "$GATEWAYS" --base-port "$BASE_PORT" --lanes "$LANES" \
  --forward-threshold "$FORWARD_THRESHOLD" --shard-factor "$SHARD_FACTOR" \
  --publisher-multiplier "$PUBLISHER_MULTIPLIER" --mesh-degree "$MESH_DEGREE"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
if [ "$SKIP_BUILD" != "1" ]; then
  log "building binaries"
  mkdir -p bin
  # Static builds: the image is Alpine (musl) and the host toolchain is glibc,
  # so a cgo build would link against an interpreter the image does not have.
  ( cd "$GATEWAY_REPO" && CGO_ENABLED=0 go build -o "$OLDPWD/bin/optimum-gateway" ./cmd )
  [ -d "$BENCH_REPO" ] || fail "optimum-bench-v2 not found at $BENCH_REPO (set SCALE_BENCH_REPO)"
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
$COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true

log "starting the control plane (bootstrap, config proxy, collector)"
$COMPOSE up -d bootstrap configproxy otel-collector

# Capture the collector's stdout from before any traffic exists. The debug
# exporter prints spans through the collector's logger, so this file is the only
# record of them; `logs -f` drains it continuously into the artifact directory.
log "capturing spans to $OUT/otel-collector.log"
: > "$OUT/otel-collector.log"
$COMPOSE logs -f --no-log-prefix otel-collector >> "$OUT/otel-collector.log" 2>&1 &
TAIL_PID=$!

# Gateways start one at a time and each is waited for. A gateway registers
# itself and then asks the bootstrap for peers exactly once, and never again, so
# the start order is what decides the topology. Starting gateway N only once
# gateway N-1 serves HTTP makes that topology reproducible rather than whatever
# the startup race produced.
for i in $(seq 1 "$GATEWAYS"); do
  port=$((BASE_PORT + i - 1))
  log "starting gateway$i of $GATEWAYS"
  $COMPOSE up -d "gateway$i"
  ready=0
  for _ in $(seq 1 120); do
    if curl -sf -m 2 "http://127.0.0.1:$port/api/v1/self_info" >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  [ "$ready" = 1 ] || { $COMPOSE logs "gateway$i" | tail -30; fail "gateway$i did not come up"; }
  sleep 0.3
done

# ---------------------------------------------------------------------------
# Wait for the topology to settle
# ---------------------------------------------------------------------------
# Not a full mesh, and deliberately so: the gateway asks the bootstrap for at
# most 7 peers, exactly once. Above 8 gateways the result is a random graph, not
# a clique, so the condition is "every node has the peers it is ever going to
# have and a confirmed datagram path to each", detected as a total that stops
# moving.
min_peers=$((GATEWAYS - 1))
if [ "$min_peers" -gt 7 ]; then min_peers=7; fi

log "waiting for the topology to settle (>= $min_peers peers each, every path confirmed)"
stable=0; last_total=-1; settled=0
for _ in $(seq 1 90); do
  total=0; ok=1
  for i in $(seq 1 "$GATEWAYS"); do
    port=$((BASE_PORT + i - 1))
    triple=$(curl -sf -m 2 "http://127.0.0.1:$port/api/v1/self_info" \
      | python3 -c 'import json,sys
d=json.load(sys.stdin); g=d["datagram"]
print(d["mump2p"]["total_peers"], g.get("paths_confirmed",0), g.get("peers_total",0))' 2>/dev/null || true)
    [ -n "$triple" ] || triple="0 0 0"
    read -r n c t <<<"$triple"
    total=$((total + n))
    if [ "$n" -lt "$min_peers" ] || [ "$c" != "$t" ] || [ "$t" -eq 0 ]; then ok=0; fi
  done
  if [ "$ok" = 1 ] && [ "$total" = "$last_total" ]; then
    stable=$((stable + 1))
    if [ "$stable" -ge 3 ]; then settled=1; break; fi
  else
    stable=0
  fi
  last_total="$total"
  sleep 2
done
if [ "$settled" = 1 ]; then
  printf 'settled: %d peer edges across %d gateways (mean degree %s)\n' \
    "$last_total" "$GATEWAYS" "$(python3 -c "print(f'{$last_total/$GATEWAYS:.1f}')")"
else
  printf 'WARNING: the topology never settled; publishing anyway, the report shows the peer counts\n'
fi

# ---------------------------------------------------------------------------
# Publish
# ---------------------------------------------------------------------------
log "publishing $MSG_COUNT beacon blocks through gateway1's CL ingress (warmup $WARMUP, one per 12s slot)"
$COMPOSE up -d publisher
publish_deadline=$(( $(date +%s) + 90 + MSG_COUNT * 15 ))
while $COMPOSE ps --status running --services 2>/dev/null | grep -qx publisher; do
  [ "$(date +%s)" -lt "$publish_deadline" ] || fail "the publisher did not finish in time"
  sleep 2
done
$COMPOSE logs publisher 2>&1 | tail -3

log "settling for ${SETTLE}s (last decodes, then the span batch export)"
sleep "$SETTLE"
sync

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
log "reporting"
cp served-config.json "$OUT/served-config.json"

# Spans are tagged with the mump2p node id, which is that node's own peer ID and
# appears nowhere in self_info or /metrics. The gateway logs it once at startup,
# so the mapping to service names is read back from the container logs.
{
  printf '{'
  sep=""
  for i in $(seq 1 "$GATEWAYS"); do
    nid=$($COMPOSE logs "gateway$i" 2>/dev/null \
      | grep -m1 'mump2p node identity' \
      | sed -n 's/.*"node_id":"\([^"]*\)".*/\1/p') || true
    [ -n "$nid" ] || continue
    printf '%s"%s":"gateway%s"' "$sep" "$nid" "$i"
    sep=","
  done
  printf '}\n'
} > "$OUT/node-map.json"

set +e
python3 ./report.py \
  --gateways "$GATEWAYS" --base-port "$BASE_PORT" \
  --expect-messages "$MSG_COUNT" \
  --spans "$OUT/otel-collector.log" \
  --node-map "$OUT/node-map.json" \
  --json "$OUT/report.json" \
  | tee "$OUT/report.txt"
rc=${PIPESTATUS[0]}
set -e

# ---------------------------------------------------------------------------
# Shut down cleanly
# ---------------------------------------------------------------------------
if [ "$KEEP_UP" = 1 ]; then
  log "leaving the stack up (--keep-up); tear it down with: $COMPOSE down -v"
else
  # SIGTERM, never a kill: the gateways' span processor flushes for 5s on
  # shutdown, and killing them drops the tail of the trace.
  log "stopping (SIGTERM, so the span flush runs)"
  $COMPOSE stop
  sleep 8
  $COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true
fi
cleanup; TAIL_PID=""

log "artifacts in $OUT"
ls -la "$OUT"
if [ "$rc" -ne 0 ]; then
  printf '\nrun.sh: the run completed but its preconditions did not hold; see %s/report.txt\n' "$OUT"
fi
exit "$rc"
