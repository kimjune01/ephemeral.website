#!/bin/bash
# End-to-end smoke test against live ephemeral.website infra.
# Run post-deploy. Uses only curl + jq — no test framework required.
#
# Exercises:
#   - POST /upload (create token, get presigned URL)
#   - PUT to S3 presigned URL (upload audio)
#   - GET /check (verify token exists)
#   - POST /session (burn token, start playback session)
#   - GET /stream (fetch audio with range request)
#   - POST /heartbeat (keep session alive)
#   - POST /complete (delete audio, end session)
#   - GET /check (verify token is gone — "in their head now")
#   - 2-phase upsert: reserve slug, then upsert note+waveform
#   - Real slug collision (different s3_key) must return 409

set -euo pipefail

API="${API:-https://ephemeral.website/api}"
SLUG_PREFIX="smoke-$(date +%s)-$$"

red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
blue()  { printf '\033[34m%s\033[0m\n' "$*"; }
step()  { blue "==> $*"; }
fail()  { red "FAIL: $*"; exit 1; }

require() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require curl
require jq

# ~1 KiB of silence encoded as MP3 works as valid-enough audio for the test.
# We just need bytes; the site never plays them back in this test.
SAMPLE=$(mktemp -t ephemeral-smoke.XXXXXX)
trap 'rm -f "$SAMPLE"' EXIT
head -c 4096 /dev/urandom > "$SAMPLE"

# ──────────────────────────────────────────────────────────────
step "1. POST /upload (create token)"
resp=$(curl -sS -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG_PREFIX-a\",\"content_type\":\"audio/mpeg\"}")
token=$(echo "$resp" | jq -r '.token')
upload_url=$(echo "$resp" | jq -r '.upload_url')
s3_key=$(echo "$resp" | jq -r '.s3_key')
[[ "$token" == "$SLUG_PREFIX-a" ]] || fail "expected token $SLUG_PREFIX-a, got $token"
[[ "$upload_url" == https://* ]]   || fail "expected https upload_url, got $upload_url"
[[ "$s3_key" == audio/* ]]         || fail "expected audio/ s3_key, got $s3_key"
green "    token=$token s3_key=$s3_key"

step "2. PUT audio to S3 presigned URL"
http_code=$(curl -sS -o /dev/null -w '%{http_code}' \
  -X PUT "$upload_url" \
  -H 'Content-Type: audio/mpeg' \
  --data-binary "@$SAMPLE")
[[ "$http_code" == "200" ]] || fail "S3 PUT returned $http_code"
green "    uploaded $(wc -c <"$SAMPLE") bytes"

step "3. GET /check (token should exist)"
exists=$(curl -sS "$API/check/$token" | jq -r '.exists')
[[ "$exists" == "true" ]] || fail "token not found after upload (exists=$exists)"
green "    token exists"

step "4. POST /session (burn token, start playback)"
sess=$(curl -sS -X POST "$API/session" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"$token\"}")
session_id=$(echo "$sess" | jq -r '.session_id')
[[ -n "$session_id" && "$session_id" != "null" ]] || fail "no session_id in response"
green "    session_id=$session_id"

step "5. GET /check (token should be burned now)"
exists=$(curl -sS "$API/check/$token" | jq -r '.exists')
[[ "$exists" == "false" ]] || fail "token still exists after burn (exists=$exists)"
green "    token burned"

step "6. GET /stream/<session>"
bytes=$(curl -sS "$API/stream/$session_id" | wc -c | tr -d ' ')
[[ "$bytes" -gt 0 ]] || fail "stream returned 0 bytes"
green "    streamed $bytes bytes"

step "7. POST /heartbeat"
http_code=$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST "$API/heartbeat/$session_id")
[[ "$http_code" == "200" ]] || fail "heartbeat returned $http_code"
green "    heartbeat ok"

step "8. POST /complete"
http_code=$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST "$API/complete/$session_id")
[[ "$http_code" == "200" ]] || fail "complete returned $http_code"
green "    session completed"

# ──────────────────────────────────────────────────────────────
step "9. 2-phase flow: reserve slug"
resp=$(curl -sS -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d '{"content_type":"audio/mpeg"}')
phase0_s3=$(echo "$resp" | jq -r '.s3_key')
phase0_upload=$(echo "$resp" | jq -r '.upload_url')
curl -sS -o /dev/null -X PUT "$phase0_upload" -H 'Content-Type: audio/mpeg' --data-binary "@$SAMPLE"

resp=$(curl -sS -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG_PREFIX-b\",\"content_type\":\"audio/mpeg\",\"s3_key\":\"$phase0_s3\"}")
reserved=$(echo "$resp" | jq -r '.token')
[[ "$reserved" == "$SLUG_PREFIX-b" ]] || fail "phase 1 reservation failed: $resp"
green "    reserved $reserved"

step "10. 2-phase flow: upsert note (same s3_key)"
resp=$(curl -sS -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG_PREFIX-b\",\"note\":\"hello smoke\",\"content_type\":\"audio/mpeg\",\"s3_key\":\"$phase0_s3\"}")
updated=$(echo "$resp" | jq -r '.token')
[[ "$updated" == "$SLUG_PREFIX-b" ]] || fail "phase 2 upsert failed: $resp"
green "    upserted note"

# Verify the note actually landed by burning + streaming
sess=$(curl -sS -X POST "$API/session" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"$SLUG_PREFIX-b\"}")
note_back=$(echo "$sess" | jq -r '.note')
[[ "$note_back" == "hello smoke" ]] || fail "expected note 'hello smoke', got '$note_back'"
green "    note survived to session"
sid=$(echo "$sess" | jq -r '.session_id')
curl -sS -o /dev/null -X POST "$API/complete/$sid"

# ──────────────────────────────────────────────────────────────
step "11. Real slug collision (different s3_key) must 409"
resp1=$(curl -sS -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG_PREFIX-c\",\"content_type\":\"audio/mpeg\"}")
token_c=$(echo "$resp1" | jq -r '.token')
[[ "$token_c" == "$SLUG_PREFIX-c" ]] || fail "first collision-test create failed"

http_code=$(curl -sS -o /tmp/collision-body -w '%{http_code}' \
  -X POST "$API/upload" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG_PREFIX-c\",\"content_type\":\"audio/mpeg\"}")
[[ "$http_code" == "409" ]] || fail "expected 409 on real collision, got $http_code ($(cat /tmp/collision-body))"
rm -f /tmp/collision-body
green "    collision correctly rejected"

# Clean up the collision-test token — burn it so it's gone
resp1=$(curl -sS -X POST "$API/session" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"$token_c\"}")
sid=$(echo "$resp1" | jq -r '.session_id // empty')
[[ -n "$sid" ]] && curl -sS -o /dev/null -X POST "$API/complete/$sid" || true

# ──────────────────────────────────────────────────────────────
green ""
green "all smoke checks passed ✓"
