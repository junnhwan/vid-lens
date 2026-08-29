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
: "${EXPECTED_RUNTIME_GENERATION:?EXPECTED_RUNTIME_GENERATION is required}"

[[ "$GITHUB_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]] \
  || die 'GITHUB_SHA must contain 7-64 hexadecimal characters'
[[ "$EXPECTED_RUNTIME_GENERATION" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]] \
  || die 'EXPECTED_RUNTIME_GENERATION is not a safe marker value'

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

service_name="${SERVICE_NAME:-vidlens.service}"
health_url="${HEALTH_URL:-http://127.0.0.1:18083/readyz}"
marker_file="$deploy_dir/.runtime-generation"

[ -f "$tmp_dir/server" ] || die "missing server artifact: $tmp_dir/server"
[ -s "$tmp_dir/server" ] || die "empty server artifact: $tmp_dir/server"
[ -f "$marker_file" ] || die "runtime migration marker missing: $marker_file"
actual_generation="$(cat "$marker_file")"
# Command substitution strips trailing LF characters; remove one optional CR so
# an operator-created CRLF marker is accepted without concatenating lines.
actual_generation="${actual_generation%$'\r'}"
[ "$actual_generation" = "$EXPECTED_RUNTIME_GENERATION" ] \
  || die "runtime migration marker mismatch: expected $EXPECTED_RUNTIME_GENERATION"

backup_dir="$deploy_dir/.logs/deploy-backups/$stamp"
staged_server="$deploy_dir/server.new-$stamp"
failed_server="$deploy_dir/server.failed-$stamp"

for path in "$backup_dir" "$staged_server" "$failed_server"; do
  [ ! -e "$path" ] || die "deployment path already exists: $path"
done

had_server=0
activation_started=0
service_restart_attempted=0

rollback_on_error() {
  local status="$1"
  local line="$2"
  trap - ERR
  set +e

  if [ "$activation_started" -ne 1 ]; then
    warn "Deployment failed before activation at line $line; current release was not replaced"
    exit "$status"
  fi

  warn "Deployment failed at line $line; restoring previous server from $backup_dir"
  local rollback_ok=1

  if [ "$had_server" -eq 1 ] && [ -f "$backup_dir/server" ]; then
    install -m 0755 "$backup_dir/server" "$deploy_dir/server.rollback-$stamp" \
      && mv "$deploy_dir/server.rollback-$stamp" "$deploy_dir/server" \
      || rollback_ok=0
  else
    rm -f -- "$deploy_dir/server" || rollback_ok=0
  fi

  rm -f -- "$staged_server" "$failed_server"

  if [ "$had_server" -eq 1 ]; then
    systemctl restart "$service_name" || rollback_ok=0
    systemctl is-active --quiet "$service_name" || rollback_ok=0
  elif [ "$service_restart_attempted" -eq 1 ]; then
    systemctl stop "$service_name" || rollback_ok=0
  fi

  if [ "$rollback_ok" -eq 1 ]; then
    warn "Rollback restored previous server; failed artifacts retained at $tmp_dir"
  else
    warn "Rollback was incomplete; inspect $backup_dir and service state immediately"
  fi
  exit "$status"
}
trap 'rollback_on_error $? $LINENO' ERR

mkdir -p "$backup_dir"
if [ -f "$deploy_dir/server" ]; then
  had_server=1
  cp -p "$deploy_dir/server" "$backup_dir/server"
fi
if [ -f "$deploy_dir/config.yaml" ]; then
  cp -p "$deploy_dir/config.yaml" "$backup_dir/config.yaml"
fi
cat > "$backup_dir/deployment-metadata.txt" <<METADATA
requested_sha=$GITHUB_SHA
runtime_generation=$EXPECTED_RUNTIME_GENERATION
previous_server_present=$had_server
METADATA

install -m 0755 "$tmp_dir/server" "$staged_server"

# Activate only after validation and staging have completed.
activation_started=1
if [ "$had_server" -eq 1 ]; then
  mv "$deploy_dir/server" "$failed_server"
fi
mv "$staged_server" "$deploy_dir/server"

service_restart_attempted=1
systemctl restart "$service_name"
systemctl is-active --quiet "$service_name"
curl -fsS --retry 10 --retry-delay 2 --retry-connrefused "$health_url"

activation_started=0
trap - ERR

if ! rm -rf -- "$tmp_dir"; then
  warn "Deployment succeeded but uploaded artifacts could not be removed: $tmp_dir"
fi
if [ -e "$failed_server" ] && ! rm -f -- "$failed_server"; then
  warn "Deployment succeeded but previous server could not be removed: $failed_server"
fi

log "Deployed server $sha with runtime generation $EXPECTED_RUNTIME_GENERATION; backup saved to $backup_dir"
