#!/usr/bin/env bash
# Build all tailtap targets with a Tailscale auth key baked in.
#
# Supply the key one of three ways:
#   ./build.sh tskey-auth-xxxx                       # pass it explicitly
#   KEY=tskey-auth-xxxx ./build.sh                   # via env
#   TS_CLIENT_ID=... TS_CLIENT_SECRET=... ./build.sh # auto-mint via OAuth (mint-key.sh)
#
# The key ends up INSIDE each binary — treat every file in dist/ as a secret.
set -euo pipefail

cd "$(dirname "$0")"

KEY="${1:-${KEY:-}}"
if [[ -z "$KEY" ]]; then
  if [[ -n "${TS_CLIENT_ID:-}" && -n "${TS_CLIENT_SECRET:-}" ]]; then
    echo "no key given — minting one via OAuth (mint-key.sh)..." >&2
    KEY="$(./mint-key.sh)"
  else
    echo "usage: $0 <tskey-auth-...>   (or set KEY, or set TS_CLIENT_ID + TS_CLIENT_SECRET to auto-mint)" >&2
    exit 1
  fi
fi

# Prefer a locally-installed Go SDK if it's on PATH, else fall back to ~/.local.
if ! command -v go >/dev/null 2>&1; then
  export PATH="$HOME/.local/go-sdk/go/bin:$PATH"
fi

mkdir -p dist
LDFLAGS="-s -w -X main.authKey=$KEY"

build() { # os arch out
  echo ">> $1/$2"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -ldflags "$LDFLAGS" -o "dist/$3" .
}

build linux   amd64 tailtap-linux-amd64
build linux   arm64 tailtap-linux-arm64          # Raspberry Pi etc.
build windows amd64 tailtap-windows-amd64.exe
build darwin  arm64 tailtap-macos-arm64
build darwin  amd64 tailtap-macos-amd64          # Intel Macs

echo
echo "dist/ (each carries a live key — do not commit, delete after the job):"
ls -la dist/
