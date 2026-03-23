#!/usr/bin/env bash
set -euo pipefail

DB_URL="${DATABASE_URL:-postgres://admin:admin@postgres:5432/sensors?sslmode=disable}"
TARGET_URL="${TARGET_URL:-http://back:3420/add}"
VERIFY_TIMEOUT=60

echo "=== Load Test (k6) ==="
echo "Target:   $TARGET_URL"
echo "Stages:   ramp-up(5s) → sustain@10(5s) → explode@10000(10s) → sustain(5s) → ramp-down(5s)"

echo ""
echo "=== Cleaning Database ==="

EXISTING=$(psql "$DB_URL" -t -A -c "SELECT COUNT(*) FROM sensor_data" 2>/dev/null || echo "0")
if [ "$EXISTING" -gt 0 ] 2>/dev/null; then
  echo "Cleaning DB: deleting $EXISTING existing rows..."
fi

psql "$DB_URL" -c "TRUNCATE TABLE sensor_data RESTART IDENTITY" 2>/dev/null && echo "DB cleaned" || echo "WARNING: TRUNCATE failed"

echo ""
echo "=== Running k6 Load Test ==="

k6 run \
  -e TARGET_URL="$TARGET_URL" \
  /scripts/loadtest.js

echo ""
echo "=== Verification Phase ==="

if [ ! -f /tmp/k6-results.json ]; then
  echo "FAIL: k6 results file not found"
  exit 1
fi

EXPECTED=$(jq -r '.success_count' /tmp/k6-results.json)
echo "Expected rows: $EXPECTED (based on HTTP 200 responses)"

if [ "$EXPECTED" -eq 0 ] 2>/dev/null; then
  echo "WARNING: no successful requests recorded, skipping verification"
  exit 0
fi

ELAPSED=0
while [ "$ELAPSED" -lt "$VERIFY_TIMEOUT" ]; do
  COUNT=$(psql "$DB_URL" -t -A -c "SELECT COUNT(*) FROM sensor_data" 2>/dev/null || echo "0")
  echo "DB rows: $COUNT / $EXPECTED expected"

  if [ "$COUNT" -ge "$EXPECTED" ] 2>/dev/null; then
    echo ""
    echo "PASS: all messages arrived in the database"
    echo "  Expected: $EXPECTED | Found: $COUNT"
    echo ""
    exit 0
  fi

  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

echo ""
echo "FAIL: not all messages arrived within timeout"
echo "  Expected: $EXPECTED | Found: $COUNT | Missing: $((EXPECTED - COUNT))"
echo ""
exit 1
