#!/bin/sh
set -eu
DEPLOY_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$DEPLOY_DIR"
case "$(uname -m)" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) printf 'Unsupported architecture\n' >&2; exit 1;; esac
if [ ! -f .env ]; then
  printf '%s\n' 'MEDIA_PATH=./media' 'PORT=18081' 'BIND_ADDRESS=0.0.0.0' "APP_UID=$(id -u)" "APP_GID=$(id -g)" 'IMAGE=mytiktok:latest' > .env
  chmod 600 .env
fi
mkdir -p data media backups
STAGING=$(mktemp -d "$DEPLOY_DIR/.update.XXXXXX")
trap 'rm -f "$STAGING/image.tar.gz" "$STAGING/checksum" "$STAGING/compose.release.yaml"; rmdir "$STAGING" 2>/dev/null || true' EXIT
BASE=https://github.com/Nex-Z/MyTiktok/releases/latest/download
ASSET="mytiktok-linux-$ARCH.tar.gz"
curl -fL --retry 3 "$BASE/$ASSET" -o "$STAGING/image.tar.gz"
curl -fL --retry 3 "$BASE/$ASSET.sha256" -o "$STAGING/checksum"
curl -fL --retry 3 "$BASE/compose.release.yaml" -o "$STAGING/compose.release.yaml"
EXPECTED=$(awk '{print $1}' "$STAGING/checksum")
ACTUAL=$(sha256sum "$STAGING/image.tar.gz" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || { printf 'Checksum mismatch\n' >&2; exit 1; }
# Loading an image does not stop the running container.
docker load -i "$STAGING/image.tar.gz"
if [ ! -f compose.release.yaml ]; then cp "$STAGING/compose.release.yaml" compose.release.yaml; fi
docker compose -f compose.release.yaml stop mytiktok
umask 077
ARCHIVE="backups/pre-update-$(date +%Y%m%d-%H%M%S).tar.gz"
if ! tar -czf "$ARCHIVE" data .env compose.release.yaml; then docker compose -f compose.release.yaml start mytiktok; exit 1; fi
cp "$STAGING/compose.release.yaml" compose.release.yaml
docker compose -f compose.release.yaml up -d --no-build --pull never --wait --wait-timeout 90
docker compose -f compose.release.yaml ps
printf '\nUpdated. Backup: %s\n' "$ARCHIVE"
