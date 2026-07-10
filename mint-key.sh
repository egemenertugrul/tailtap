#!/usr/bin/env bash
# Mint (or optionally reuse) a Tailscale ephemeral, tagged auth key via an
# OAuth client — no manual clicking in the admin console.
#
# Prints ONLY the key to stdout; all diagnostics go to stderr, so it composes:
#     ./build.sh "$(./mint-key.sh)"
#
# Requires: curl, jq, and a Tailscale OAuth client with the
# "Keys > Auth Keys > Write" scope and the tag below in its allowed tags.
#   https://login.tailscale.com/admin/settings/oauth
#
# Secrets (never commit these — pass via env):
#   TS_CLIENT_ID       OAuth client id
#   TS_CLIENT_SECRET   OAuth client secret
#
# Behaviour:
#   Default            mint a FRESH key every run (matches "revoke after each job").
#   TS_KEY_REUSE=1     reuse a cached key until it's within RENEW_BEFORE_DAYS of
#                      expiry (fewer keys/devices; wider blast radius per key).
#
# Tunables (env):
#   TS_TAG                    tag to stamp        (default tag:tailtap)
#   TS_KEY_EXPIRY_SECONDS     key lifetime        (default 7776000 = 90d, the max)
#   TS_KEY_RENEW_BEFORE_DAYS  renew threshold     (default 7)   [reuse mode only]
#   TS_KEY_CACHE_DIR          cache location      (default ~/.tailtap) [reuse mode only]
set -euo pipefail

: "${TS_CLIENT_ID:?set TS_CLIENT_ID (Tailscale OAuth client id)}"
: "${TS_CLIENT_SECRET:?set TS_CLIENT_SECRET (Tailscale OAuth client secret)}"
command -v curl >/dev/null || { echo "mint-key: curl not found" >&2; exit 1; }
command -v jq   >/dev/null || { echo "mint-key: jq not found"   >&2; exit 1; }

TAG="${TS_TAG:-tag:tailtap}"
EXPIRY_SECONDS="${TS_KEY_EXPIRY_SECONDS:-7776000}"
REUSE="${TS_KEY_REUSE:-0}"
RENEW_BEFORE_DAYS="${TS_KEY_RENEW_BEFORE_DAYS:-7}"
CACHE_DIR="${TS_KEY_CACHE_DIR:-$HOME/.tailtap}"
KEYFILE="$CACHE_DIR/authkey"
KEYID_FILE="$CACHE_DIR/authkey.id"

log() { echo "mint-key: $*" >&2; }

# Epoch seconds from an ISO-8601 timestamp, portable across GNU and BSD/macOS date.
to_epoch() {
  if date -d "1970-01-01T00:00:01Z" +%s >/dev/null 2>&1; then
    date -d "$1" +%s                                 # GNU
  else
    date -jf "%Y-%m-%dT%H:%M:%SZ" "${1%%.*}Z" +%s    # BSD/macOS (drop fractional secs)
  fi
}

oauth_token() {
  curl -sf -u "${TS_CLIENT_ID}:${TS_CLIENT_SECRET}" \
    https://api.tailscale.com/api/v2/oauth/token \
    -d grant_type=client_credentials | jq -er .access_token
}

mint_new() {
  local token resp
  token=$(oauth_token)
  resp=$(curl -sf -X POST -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    https://api.tailscale.com/api/v2/tailnet/-/keys \
    -d "{\"capabilities\":{\"devices\":{\"create\":{\"reusable\":true,\"ephemeral\":true,\"preauthorized\":true,\"tags\":[\"${TAG}\"]}}},\"expirySeconds\":${EXPIRY_SECONDS}}")
  mkdir -p "$CACHE_DIR" && chmod 700 "$CACHE_DIR"
  jq -er .key <<<"$resp" > "$KEYFILE" && chmod 600 "$KEYFILE"
  jq -er .id  <<<"$resp" > "$KEYID_FILE"
  log "minted new key (tag ${TAG}, expires in $((EXPIRY_SECONDS/86400))d)"
  cat "$KEYFILE"
}

if [[ "$REUSE" == "1" && -s "$KEYFILE" && -s "$KEYID_FILE" ]]; then
  token=$(oauth_token)
  keyid=$(cat "$KEYID_FILE")
  # The API returns a key's expiry (by id) but never the key string again — that
  # is why we cache the string locally at creation and only re-check expiry here.
  expires=$(curl -sf -H "Authorization: Bearer ${token}" \
    "https://api.tailscale.com/api/v2/tailnet/-/keys/${keyid}" | jq -r '.expires // "null"' 2>/dev/null || echo null)
  if [[ -n "$expires" && "$expires" != "null" ]]; then
    remain=$(( ( $(to_epoch "$expires") - $(date +%s) ) / 86400 ))
    if (( remain > RENEW_BEFORE_DAYS )); then
      log "cached key valid ${remain}d more — reusing"
      cat "$KEYFILE"; exit 0
    fi
    log "cached key expires in ${remain}d — renewing"
  else
    log "cached key gone from server (revoked/expired) — minting new"
  fi
fi

mint_new
