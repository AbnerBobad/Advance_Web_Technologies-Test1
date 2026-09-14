#!/usr/bin/env bash
# Smoke-test for the ImageLab Version 1 acceptance + job/worker lifecycle.
# Sections 1-3 cover the Week 1 acceptance boundary; section 4 covers the
# Week 2 durable job, background worker, and variant serving.
# Requires a running server (default http://localhost:4000) and test assets.
set -uo pipefail

BASE="${BASE:-http://localhost:4000}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0

check() {
  local label="$1" expected="$2" actual="$3" extra="${4:-}"
  if [[ "$expected" == "$actual" ]]; then
    PASS=$((PASS + 1))
    echo "  PASS  $label"
  else
    FAIL=$((FAIL + 1))
    echo "  FAIL  $label  (expected $expected, got $actual) $extra"
  fi
}

echo "creating test assets in $WORK"
go run ./scripts/gen_assets.go "$WORK"

echo "1) static frontend served"
check "GET / is 200" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/")"

echo "2) valid PNG accepted as 202"
BODY="$(curl -s -w '\n%{http_code}' -X POST "$BASE/v1/images" -F "file=@$WORK/photo.png")"
STATUS="${BODY##*$'\n'}"
check "POST png returns 202" 202 "$STATUS"
JSON="${BODY%$'\n'*}"
check "body has image_id" true "$(echo "$JSON" | grep -q '"image_id"' && echo true || echo false)"
check "body has job_id" true "$(echo "$JSON" | grep -q '"job_id"' && echo true || echo false)"
check "body status queued" true "$(echo "$JSON" | grep -Eq '"status"\s*:\s*"queued"' && echo true || echo false)"
check "body has status_url" true "$(echo "$JSON" | grep -q '"status_url"' && echo true || echo false)"
LOC="$(curl -s -D - -o /dev/null -X POST "$BASE/v1/images" -F "file=@$WORK/photo.jpg" | awk -F': ' 'tolower($1)=="location"{print $2}' | tr -d '\r')"
check "Location header sent" true "$([ -n "$LOC" ] && echo true || echo false)"

echo "3) validation rejections"
check "text named .png -> 415" 415 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/images" -F "file=@$WORK/fake.png")"
check "broken jpeg -> 400" 400 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/images" -F "file=@$WORK/broken.jpg")"
check "missing file -> 400" 400 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/images" -F "note=hi")"
check "oversize -> 413" 413 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/images" -F "file=@$WORK/big.bin")"

echo "4) durable job lifecycle and worker (Week 2)"
BODY="$(curl -s -X POST "$BASE/v1/images" -F "file=@$WORK/photo.png")"
JOB_ID="$(echo "$BODY" | grep -o '"job_id": "[^"]*"' | cut -d'"' -f4)"
IMAGE_ID="$(echo "$BODY" | grep -o '"image_id": "[^"]*"' | cut -d'"' -f4)"
check "202 body has job_id" true "$([ -n "$JOB_ID" ] && echo true || echo false)"
STATUS_URL="$(echo "$BODY" | grep -o '"status_url": "[^"]*"' | cut -d'"' -f4)"
check "202 body has status_url" true "$([ "$STATUS_URL" = "/v1/jobs/$JOB_ID" ] && echo true || echo false)"

# Poll until terminal; the single worker should reach completed quickly.
STATE=""
for _ in $(seq 1 40); do
  STATE="$(curl -s "$BASE$STATUS_URL" | grep -o '"status": "[^"]*"' | cut -d'"' -f4)"
  case "$STATE" in completed|failed) break ;; esac
  sleep 0.25
done
check "job reaches terminal state" completed "$STATE"
JOB="$(curl -s "$BASE$STATUS_URL")"
check "completed job has 3 variants" 3 "$(echo "$JOB" | grep -o '"name"' | wc -l)"
THUMB_W="$(echo "$JOB" | grep -A1 '"name": "thumbnail"' | grep -o '"width": [0-9]*' | cut -d' ' -f2)"
THUMB_H="$(echo "$JOB" | grep -A2 '"name": "thumbnail"' | grep -o '"height": [0-9]*' | cut -d' ' -f2)"
check "thumbnail is 150x150" "150 150" "$THUMB_W $THUMB_H"
check "hide internal columns" true "$(echo "$JOB" | grep -q 'stored_filename' && echo false || echo true)"

VURL="$(echo "$JOB" | grep -o '"url": "[^"]*"' | grep thumbnail | cut -d'"' -f4)"
check "variant served" 200 "$(curl -s -o "$WORK/v.png" -w '%{http_code}' "$BASE$VURL")"
check "variant mime is image/png" image/png "$(file -b --mime-type "$WORK/v.png")"

check "unknown job -> 404" 404 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/jobs/00000000-0000-0000-0000-000000000000")"
check "unknown variant name -> 404" 404 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/images/$IMAGE_ID/variants/original")"

echo
echo "RESULT: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]