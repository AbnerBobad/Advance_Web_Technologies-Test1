#!/usr/bin/env bash
# Smoke-test for the ImageLab Version 1 Week 1 acceptance path.
# Requires a running server (default http://localhost:8080) and test assets.
set -uo pipefail

BASE="${BASE:-http://localhost:8080}"
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

# echo "creating test assets in $WORK"
# python3 - "$WORK" <<'PY'
# import sys
# from PIL import Image
# work = sys.argv[1]
# Image.new('RGB', (1200, 800), (30, 90, 200)).save(work + '/photo.png')
# Image.new('RGB', (1200, 800), (30, 90, 200)).save(work + '/photo.jpg', 'JPEG')
# open(work + '/fake.png', 'w').write('this is not an image')
# with open(work + '/big.bin', 'wb') as f:
#     f.write(b'\0' * (11 * 1024 * 1024))
# data = open(work + '/photo.jpg', 'rb').read()
# open(work + '/broken.jpg', 'wb').write(data[:400])
# PY
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

echo
echo "RESULT: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]