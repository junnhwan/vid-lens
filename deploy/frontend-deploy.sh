#!/usr/bin/env bash
set -Eeuo pipefail

log() {
  printf '%s\n' "$*"
}

warn() {
  printf 'WARNING: %s\n' "$*" >&2
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_safe_absolute_dir() {
  local label="$1"
  local value="$2"
  case "$value" in
    /*) ;;
    *) die "$label must be an absolute path" ;;
  esac
  [ "$value" != "/" ] || die "$label must not be filesystem root"
  [ "$value" != "/tmp" ] || die "$label must not be the shared /tmp root"
  case "$value/" in
    */../*|*/./*|*//*) die "$label must be normalized without dot or empty segments" ;;
  esac
}

: "${DEPLOY_PATH:?DEPLOY_PATH is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${VIDLENS_API_BASE:?VIDLENS_API_BASE is required}"
: "${FRONTEND_HEALTH_URL:?FRONTEND_HEALTH_URL is required}"

[[ "$GITHUB_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]] \
  || die 'GITHUB_SHA must contain 7-64 hexadecimal characters'

deploy_dir="${DEPLOY_PATH%/}"
require_safe_absolute_dir DEPLOY_PATH "$deploy_dir"

sha="${GITHUB_SHA:0:12}"
stamp="${DEPLOY_STAMP:-$(date +%Y%m%d-%H%M%S)-$sha}"
[[ "$stamp" =~ ^[0-9A-Za-z._-]+$ ]] || die 'deployment stamp is unsafe'

tmp_dir="${DEPLOY_TMP_DIR:-/tmp/vidlens-deploy-${GITHUB_SHA}}"
tmp_dir="${tmp_dir%/}"
require_safe_absolute_dir DEPLOY_TMP_DIR "$tmp_dir"
[ "$tmp_dir" != "$deploy_dir" ] || die 'DEPLOY_TMP_DIR must differ from DEPLOY_PATH'
case "$tmp_dir/" in
  "$deploy_dir/"*) die 'DEPLOY_TMP_DIR must not be nested under DEPLOY_PATH' ;;
esac
case "$deploy_dir/" in
  "$tmp_dir/"*) die 'DEPLOY_TMP_DIR must not contain DEPLOY_PATH' ;;
esac

frontend_dir="$deploy_dir/frontend"
staging_dir="$deploy_dir/frontend.new-$stamp"
old_dir="$deploy_dir/frontend.old-$stamp"
backup_dir="$deploy_dir/.logs/deploy-backups/$stamp"

[ -f "$tmp_dir/frontend-src.tar.gz" ] || die "missing frontend artifact: $tmp_dir/frontend-src.tar.gz"
[ -s "$tmp_dir/frontend-src.tar.gz" ] || die "empty frontend artifact: $tmp_dir/frontend-src.tar.gz"

for path in "$staging_dir" "$old_dir"; do
  [ ! -e "$path" ] || die "deployment path already exists: $path"
done

activation_started=0

rollback_on_error() {
  local status="$1"
  local line="$2"
  trap - ERR
  set +e

  if [ "$activation_started" -ne 1 ]; then
    warn "Deployment failed before activation at line $line; current release was not replaced"
    rm -rf -- "$staging_dir"
    exit "$status"
  fi

  warn "Deployment failed at line $line; restoring previous frontend"
  if [ -d "$old_dir" ]; then
    rm -rf -- "$frontend_dir"
    mv "$old_dir" "$frontend_dir"
  fi
  rm -rf -- "$staging_dir"
  systemctl restart vidlens-web 2>/dev/null || true
  exit "$status"
}
trap 'rollback_on_error $? $LINENO' ERR

# Build in a staging dir so a failed build never touches the live release.
mkdir -p "$staging_dir"
tar -xzf "$tmp_dir/frontend-src.tar.gz" -C "$staging_dir"
(
  cd "$staging_dir"
  npm ci --no-audit --no-fund
  VIDLENS_API_BASE="$VIDLENS_API_BASE" npm run build
)
[ -d "$staging_dir/.next" ] || die "frontend build did not produce .next"

# Record a lightweight backup of the current release metadata for forensics.
mkdir -p "$backup_dir"
if [ -d "$frontend_dir" ]; then
  cp -p "$frontend_dir/package.json" "$backup_dir/" 2>/dev/null || true
  cp -p "$frontend_dir/package-lock.json" "$backup_dir/" 2>/dev/null || true
  if [ -d "$frontend_dir/.next" ]; then
    tar -czf "$backup_dir/frontend-build.tar.gz" -C "$frontend_dir" .next 2>/dev/null || true
  fi
fi

# Activate: swap the live release out, promote the new one.
activation_started=1
if [ -d "$frontend_dir" ]; then
  mv "$frontend_dir" "$old_dir"
fi
mv "$staging_dir" "$frontend_dir"

systemctl restart vidlens-web
systemctl is-active --quiet vidlens-web
curl -fsS --retry 10 --retry-delay 2 --retry-connrefused "$FRONTEND_HEALTH_URL"

activation_started=0
trap - ERR

if [ -d "$old_dir" ] && ! rm -rf -- "$old_dir"; then
  warn "Deployment succeeded but previous frontend directory could not be removed: $old_dir"
fi

log "Deployed frontend $sha (API base $VIDLENS_API_BASE); backup saved to $backup_dir"
