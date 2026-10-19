#!/usr/bin/env bash
# Section 4 experiment driver for ImageLab Version 2.
#
# It runs the five conditions from the assessment brief, each in both
# observation modes, three repetitions each (5 x 2 x 3 = 30 runs), then
# summarises the results and draws the charts.
#
#   A  Quiet job           30 s of processing with no intermediate updates
#   B  Real progress       the actual stages produced by real work
#   C1 Simulated 10 s      a simulated update phase, updates every 2 s
#   C2 Simulated 10 s      the same phase, updates every 200 ms
#   C3 C2 + 200 ms RTT     C2 plus 200 ms of added round-trip latency
#
# Each condition runs against its own server process so the recorded server
# configuration is exact. The script requires POOLING_DB_DSN in the
# environment and a free port (default 4000).
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

PORT="${PORT:-4000}"
BASE="http://localhost:${PORT}"
REPS="${REPS:-3}"
QUIET="${QUIET:-30s}"
SIM_DURATION="${SIM_DURATION:-10s}"

OUT_DIR="experiments/results"
RAW_DIR="$OUT_DIR/raw"
LOG_DIR="$OUT_DIR/logs"
CSV="$OUT_DIR/runs.csv"
ASSETS_DIR="/tmp/imagelab-experiment-assets"
IMAGE="$ASSETS_DIR/photo.png"

test -n "${POOLING_DB_DSN:-}" || { echo "error: POOLING_DB_DSN is not set"; exit 1; }

echo "cleaning previous results"
rm -rf "$OUT_DIR"
mkdir -p "$RAW_DIR" "$LOG_DIR"

echo "building server and experiment runner"
go build -o /tmp/imagelab-api ./cmd/api
go build -o /tmp/imagelab-exp ./cmd/experiment
go build -o /tmp/imagelab-summary ./cmd/experiment-summary

echo "generating test image"
mkdir -p "$ASSETS_DIR"
go run ./scripts/gen_assets.go "$ASSETS_DIR" >/dev/null

SERVER_PID=""
stop_server() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null
    wait "$SERVER_PID" 2>/dev/null
    SERVER_PID=""
  fi
}

wait_ready() {
  for _ in $(seq 1 50); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/" 2>/dev/null || true)"
    if [[ "$code" == "200" ]]; then return 0; fi
    sleep 0.2
  done
  return 1
}

# run_condition <id> <label> <server flags...>
# Modes alternate their order between conditions so neither mode is always
# first (fairness control).
RUN_INDEX=0
run_condition() {
  local id="$1" label="$2"; shift 2
  local server_flags=("$@")
  local log="$LOG_DIR/$id.log"

  RUN_INDEX=$((RUN_INDEX + 1))
  local modes
  if (( RUN_INDEX % 2 == 1 )); then
    modes=(short long)
  else
    modes=(long short)
  fi

  echo
  echo "=== condition $id: $label ==="
  echo "server flags: ${server_flags[*]:-(none)}"
  echo "mode order: ${modes[*]}"

  /tmp/imagelab-api -port "$PORT" "${server_flags[@]}" > "$log" 2>&1 &
  SERVER_PID=$!
  if ! wait_ready; then
    echo "error: server for condition $id did not become ready"
    cat "$log"
    stop_server
    return 1
  fi

  local mode
  for mode in "${modes[@]}"; do
    /tmp/imagelab-exp \
      -base "$BASE" \
      -image "$IMAGE" \
      -condition "$id" \
      -label "$label" \
      -mode "$mode" \
      -reps "$REPS" \
      -quiet-delay "$(quiet_ms "$id")" \
      -simulate-duration "$(sim_ms "$id")" \
      -simulate-interval "$(interval_ms "$id")" \
      -latency "$(latency_ms "$id")" \
      -server-log "$log" \
      -out "$CSV" \
      -rawdir "$RAW_DIR"
  done

  stop_server
}

quiet_ms() {
  case "$1" in
    A) echo 30000 ;;
    *) echo 0 ;;
  esac
}
sim_ms() {
  case "$1" in
    C1|C2|C3) echo 10000 ;;
    *) echo 0 ;;
  esac
}
interval_ms() {
  case "$1" in
    C1) echo 2000 ;;
    C2|C3) echo 200 ;;
    *) echo 0 ;;
  esac
}
latency_ms() {
  case "$1" in
    C3) echo 200 ;;
    *) echo 0 ;;
  esac
}

# Condition A: quiet job (30 s with no updates).
run_condition A "Quiet job (30s, no updates)" -quiet-delay "$QUIET"

# Condition B: real progress only (default server configuration).
run_condition B "Real progress"

# Condition C1: simulated 10 s phase, updates every 2 s.
run_condition C1 "Simulated 10s @ 2s" -simulate-duration "$SIM_DURATION" -simulate-interval 2s

# Condition C2: simulated 10 s phase, updates every 200 ms.
run_condition C2 "Simulated 10s @ 200ms" -simulate-duration "$SIM_DURATION" -simulate-interval 200ms

# Condition C3: C2 plus 200 ms added round-trip latency in the client.
run_condition C3 "C2 + 200ms RTT" -simulate-duration "$SIM_DURATION" -simulate-interval 200ms

echo
echo "summarising and drawing charts"
/tmp/imagelab-summary -csv "$CSV" -outdir "$OUT_DIR"

echo
echo "done. results in $OUT_DIR"
echo "  runs:    $CSV ($(( $(wc -l < "$CSV") - 1 )) data rows)"
echo "  summary: $OUT_DIR/summary.csv"
echo "  charts:  $OUT_DIR/chart-detection.svg, $OUT_DIR/chart-requests.svg"
echo "  raw:     $RAW_DIR"
echo "  logs:    $LOG_DIR"
