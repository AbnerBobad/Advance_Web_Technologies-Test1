#!/usr/bin/env bash
# ImageLab Week 4 measurement harness (Section 15).
#
# Measures acknowledgement latency, queue wait, processing duration, job
# duration, polling count, and detection delay for one image, then submits
# five images close together so one worker exposes queueing.
#
# Usage:
#   BASE=http://localhost:4000 ./scripts/measure.sh
#
# For a meaningful burst, start the server with an artificial delay so later
# jobs plainly wait in the queue:
#   go run ./cmd/api -processing-delay=2s
set -uo pipefail

BASE="${BASE:-http://localhost:4000}"
POLL_SLEEP="${POLL_SLEEP:-0.5}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

field() {
  echo "$1" | grep -o "\"$2\": *\"[^\"]*\"" | head -1 | cut -d'"' -f4
}

ts_ms() {
  # RFC3339 timestamp -> epoch milliseconds. GNU date cannot parse the
  # fractional-seconds form the API returns, so split it at the decimal
  # point: whole seconds are converted by date, then the leading millisecond
  # digits of the fraction are added back.
  local digits fraction whole frac_ms
  digits="$(echo "$1" | sed -E 's/^[^.]*\.([0-9]+).*/\1/')"
  if [[ "$digits" == "$1" ]]; then
    # no fractional part
    date -d "$1" +%s%3N
    return
  fi
  fraction="${digits}000"
  frac_ms="${fraction:0:3}"
  whole="$(echo "$1" | sed -E 's/\.[0-9]+//')"
  echo $(( $(date -d "$whole" +%s) * 1000 + 10#$frac_ms ))
}

now_ms() {
  # Wall-clock epoch milliseconds. date +%s%3N is unreliable here; the
  # seconds+nanoseconds form is (epoch-seconds, first three nano digits).
  date +%s%N | cut -c1-13
}

fmt_ms() {
  local ms="$1"
  if (( ms >= 1000 )); then
    awk "BEGIN{printf \"%.2f s\", $ms/1000}"
  else
    echo "${ms} ms"
  fi
}

echo "generating fixtures in $WORK"
go run ./scripts/gen_assets.go "$WORK"

echo
echo "=== 1) single image: timing + polling ==="

ACK_RAW="$(curl -s -o "$WORK/post.json" -w '%{time_total}' \
  -X POST "$BASE/v1/images" -F "file=@$WORK/burst_2.png")"
ACK_MS="$(awk "BEGIN{printf \"%.0f\", $ACK_RAW*1000}")"

STATUS_URL="$(field "$(cat "$WORK/post.json")" status_url)"

COUNT=0
STATUS=""
BODY=""
OBSERVED_MS=""
for _ in $(seq 1 400); do
  COUNT=$((COUNT + 1))
  BODY="$(curl -s "$BASE$STATUS_URL")"
  STATUS="$(field "$BODY" status)"
  if [[ "$STATUS" == "completed" || "$STATUS" == "failed" ]]; then
    OBSERVED_MS="$(now_ms)"
    break
  fi
  sleep "$POLL_SLEEP"
done

QUEUED="$(field "$BODY" queued_at)"
STARTED="$(field "$BODY" started_at)"
COMPLETED="$(field "$BODY" completed_at)"
FAILED="$(field "$BODY" failed_at)"
END="$COMPLETED"
[[ -z "$END" ]] && END="$FAILED"

Q_WAIT=$(( $(ts_ms "$STARTED") - $(ts_ms "$QUEUED") ))
PROC=$(( $(ts_ms "$END") - $(ts_ms "$STARTED") ))
JOB_DUR=$(( $(ts_ms "$END") - $(ts_ms "$QUEUED") ))
DETECT=$(( OBSERVED_MS - $(ts_ms "$COMPLETED") ))

printf "%-26s %s\n" "status" "$STATUS"
printf "%-26s %s\n" "acknowledgement latency" "$(fmt_ms "$ACK_MS")"
printf "%-26s %s\n" "queue wait" "$(fmt_ms "$Q_WAIT")"
printf "%-26s %s\n" "processing duration" "$(fmt_ms "$PROC")"
printf "%-26s %s\n" "job duration" "$(fmt_ms "$JOB_DUR")"
printf "%-26s %s\n" "polling count" "$COUNT"
printf "%-26s %s\n" "detection delay" "$(fmt_ms "$DETECT")"

echo
echo "  variant contract (source is burst_2.png, 2000x500):"
for NAME in thumbnail preview display; do
  W="$(echo "$BODY" | grep -A2 "\"name\": \"$NAME\"" | grep -o '\"width\": [0-9]*' | head -1 | cut -d' ' -f2)"
  H="$(echo "$BODY" | grep -A4 "\"name\": \"$NAME\"" | grep -o '\"height\": [0-9]*' | head -1 | cut -d' ' -f2)"
  printf "  %-10s %s x %s\n" "$NAME" "$W" "$H"
done

echo
echo "=== 2) five-image burst (queueing behind one worker) ==="

# Submit all five as close together as the loop allows, recording each
# acknowledgement, then poll every job afterwards. This is what makes the
# later jobs wait: one worker, four jobs queued behind the first.
BURST_BASE="$(now_ms)"
echo "  submitting five images ..."
_STATUS_URLS=()
_ACKS=()
for i in 1 2 3 4 5; do
  ACK_RAW="$(curl -s -o "$WORK/b$i.json" -w '%{time_total}' \
    -X POST "$BASE/v1/images" -F "file=@$WORK/burst_$i.png")"
  _ACKS[$i]="$(awk "BEGIN{printf \"%.0f\", $ACK_RAW*1000}")"
  _STATUS_URLS[$i]="$(field "$(cat "$WORK/b$i.json")" status_url)"
done
SUBMIT_END="$(now_ms)"

echo "  submit order | file         | queue wait | processing | job duration | ack"
for i in 1 2 3 4 5; do
  B=""
  for _ in $(seq 1 400); do
    B="$(curl -s "$BASE${_STATUS_URLS[$i]}")"
    ST="$(field "$B" status)"
    [[ "$ST" == "completed" || "$ST" == "failed" ]] && break
    sleep "$POLL_SLEEP"
  done

  BQ="$(field "$B" queued_at)"
  BS="$(field "$B" started_at)"
  BC="$(field "$B" completed_at)"
  BF="$(field "$B" failed_at)"
  BE="$BC"
  [[ -z "$BE" ]] && BE="$BF"

  BW=$(( $(ts_ms "$BS") - $(ts_ms "$BQ") ))
  BP=$(( $(ts_ms "$BE") - $(ts_ms "$BS") ))
  BD=$(( $(ts_ms "$BE") - $(ts_ms "$BQ") ))

  printf "  %-11s | %-11s | %-10s | %-10s | %-12s | %s ms\n" \
    "#$i" "burst_$i.png" "$(fmt_ms "$BW")" "$(fmt_ms "$BP")" "$(fmt_ms "$BD")" "${_ACKS[$i]}"
done
END_MS="$(now_ms)"
echo
echo "  time to submit all five: $(fmt_ms $((SUBMIT_END - BURST_BASE)))"
echo "  wall time, first submission to last completion: $(fmt_ms $((END_MS - BURST_BASE)))"
echo "  (rising queue wait shows later jobs waiting behind the single worker)"