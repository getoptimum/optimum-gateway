#!/usr/bin/env bash
# Sweep one axis and print the resulting delivery-versus-x curve.
#
#   sweep.sh gateways 5 8 10 12        one run per gateway count
#   sweep.sh threshold 0.25 0.5 0.75   one run per forwarding threshold f
#   sweep.sh mesh 4 6 8                one run per mesh degree target
#
# Every run writes its own artifact directory under out/, and the summary table
# is assembled from the report.json each run leaves behind, so a sweep can be
# re-summarised later without re-running anything:
#
#   ./summarise.py out/*/report.json
#
# Knobs are the ones run.sh takes, passed through the environment. Sweeps
# default to fewer messages than a single run, because the shape of the curve
# needs runs, not blocks:
#
#   SCALE_MSG_COUNT=4 SCALE_GATEWAYS=12 ./sweep.sh threshold 0.5 0.75 0.9
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

axis="${1:-}"; shift || true
[ -n "$axis" ] && [ $# -gt 0 ] || { sed -n '2,20p' "$0"; exit 2; }

export SCALE_MSG_COUNT="${SCALE_MSG_COUNT:-4}"

case "$axis" in
  gateways|threshold|mesh) ;;
  *) printf 'sweep.sh: unknown axis %s\n' "$axis" >&2; exit 2 ;;
esac

# The binaries and the image do not change across a sweep, so only the first
# point builds them. SCALE_SKIP_BUILD=1 in the environment skips even that.
build_flag=()
if [ "${SCALE_SKIP_BUILD:-0}" = "1" ]; then build_flag=(--skip-build); fi

reports=()
for value in "$@"; do
  # The tag carries every knob that moves, not just the swept one, so two sweeps
  # along different axes cannot overwrite each other's artifacts.
  n="${SCALE_GATEWAYS:-12}"; f="${SCALE_FORWARD_THRESHOLD:-0.75}"; d="${SCALE_MESH_DEGREE:-6}"
  case "$axis" in
    gateways)  n="$value"; args=(--gateways "$value") ;;
    threshold) f="$value"; args=(--forward-threshold "$value") ;;
    mesh)      d="$value"; args=(--mesh-degree "$value") ;;
  esac
  tag="sweep-n${n}-f${f}-d${d}"
  printf '\n=== sweep %s=%s -> out/%s\n' "$axis" "$value" "$tag"
  # A single point failing its preconditions must not abandon the curve.
  ./run.sh --tag "$tag" "${build_flag[@]+"${build_flag[@]}"}" "${args[@]}" \
    || printf 'sweep.sh: %s=%s did not complete cleanly\n' "$axis" "$value"
  reports+=("out/$tag/report.json")
  build_flag=(--skip-build)
done

printf '\n=== delivery versus %s\n' "$axis"
python3 ./summarise.py "${reports[@]}"
