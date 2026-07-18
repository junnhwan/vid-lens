#!/usr/bin/env bash
set -Eeuo pipefail

# Read-only remote evidence collector for the PostgreSQL + pgvector cutover.
# It deliberately reports file metadata rather than config contents and never
# calls docker inspect, systemctl cat, or commands that expose environment data.

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

emit() {
  local key="$1"
  local value="$2"
  [[ "$key" =~ ^[A-Za-z0-9._-]+$ ]] || fail "unsafe audit key: $key"
  value="${value//$'\r'/ }"
  value="${value//$'\n'/ }"
  printf '%s=%s\n' "$key" "$value"
}

require_safe_absolute_dir() {
  local label="$1"
  local value="$2"
  case "$value" in
    /*) ;;
    *) fail "$label must be an absolute path" ;;
  esac
  [ "$value" != "/" ] || fail "$label must not be filesystem root"
  case "$value/" in
    */../*|*/./*|*//*) fail "$label must be normalized without dot or empty segments" ;;
  esac
}

collection_complete=true
mark_incomplete() {
  collection_complete=false
}

deploy_dir="${DEPLOY_PATH:-/opt/vidlens}"
deploy_dir="${deploy_dir%/}"
data_root="${DATA_ROOT:-$deploy_dir/data}"
data_root="${data_root%/}"
service_name="${SERVICE_NAME:-vidlens.service}"
local_base_url="${LOCAL_BASE_URL:-http://127.0.0.1:18083}"
local_base_url="${local_base_url%/}"

require_safe_absolute_dir DEPLOY_PATH "$deploy_dir"
require_safe_absolute_dir DATA_ROOT "$data_root"
[[ "$service_name" =~ ^[A-Za-z0-9_.@-]+\.service$ ]] \
  || fail 'SERVICE_NAME is not a safe systemd service name'
[[ "$local_base_url" =~ ^https?://(127\.0\.0\.1|localhost)(:[0-9]{1,5})?$ ]] \
  || fail 'LOCAL_BASE_URL must be a loopback HTTP(S) origin'

emit audit.version 1
emit audit.timestamp_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
emit audit.hostname "$(hostname)"
emit deploy.path "$deploy_dir"
emit data.path "$data_root"
emit service.name "$service_name"

file_evidence() {
  local label="$1"
  local path="$2"
  if [ ! -e "$path" ]; then
    emit "$label.exists" false
    return
  fi

  emit "$label.exists" true
  local metadata
  if metadata="$(stat -Lc '%s|%a|%U|%G|%Y' -- "$path" 2>/dev/null)"; then
    local bytes mode owner group modified_epoch
    IFS='|' read -r bytes mode owner group modified_epoch <<< "$metadata"
    emit "$label.bytes" "$bytes"
    emit "$label.mode" "$mode"
    emit "$label.owner" "$owner"
    emit "$label.group" "$group"
    emit "$label.modified_epoch" "$modified_epoch"
  else
    emit "$label.metadata" '<unavailable>'
    mark_incomplete
  fi

  if [ -f "$path" ] && command -v sha256sum >/dev/null 2>&1; then
    if checksum="$(sha256sum -- "$path" 2>/dev/null | awk 'NR == 1 { print $1 }')" \
      && [[ "$checksum" =~ ^[0-9a-fA-F]{64}$ ]]; then
      emit "$label.sha256" "${checksum,,}"
    else
      emit "$label.sha256" '<unavailable>'
      mark_incomplete
    fi
  elif [ -f "$path" ]; then
    emit "$label.sha256" '<unavailable>'
    mark_incomplete
  fi
}

file_evidence artifact.server "$deploy_dir/server"
file_evidence artifact.config "$deploy_dir/config.yaml"
if [ -d "$deploy_dir/web/dist" ]; then
  emit artifact.web_dist.exists true
else
  emit artifact.web_dist.exists false
fi

marker_file="$deploy_dir/.runtime-generation"
if [ -f "$marker_file" ]; then
  emit runtime.marker.exists true
  marker_value="$(cat -- "$marker_file")"
  marker_value="${marker_value%$'\r'}"
  if [[ "$marker_value" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]]; then
    emit runtime.marker.value "$marker_value"
  else
    emit runtime.marker.value '<invalid>'
  fi
else
  emit runtime.marker.exists false
  emit runtime.marker.value '<missing>'
fi

for data_name in mysql postgres redis minio milvus; do
  data_dir="$data_root/$data_name"
  if [ ! -e "$data_dir" ]; then
    emit "data.$data_name.exists" false
    continue
  fi
  emit "data.$data_name.exists" true
  if data_kib="$(du -sk -- "$data_dir" 2>/dev/null | awk 'NR == 1 { print $1 }')" \
    && [[ "$data_kib" =~ ^[0-9]+$ ]]; then
    emit "data.$data_name.kib" "$data_kib"
  else
    emit "data.$data_name.kib" '<unavailable>'
    mark_incomplete
  fi
done

if command -v df >/dev/null 2>&1; then
  if disk_output="$(df -Pk -- "$deploy_dir" 2>/dev/null | awk 'NR == 2 { print $2 "|" $3 "|" $4 "|" $5 }')" \
    && [[ "$disk_output" =~ ^[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+%$ ]]; then
    IFS='|' read -r disk_total_kib disk_used_kib disk_available_kib disk_use_percent \
      <<< "$disk_output"
    emit disk.collector_available true
    emit disk.total_kib "$disk_total_kib"
    emit disk.used_kib "$disk_used_kib"
    emit disk.available_kib "$disk_available_kib"
    emit disk.use_percent "$disk_use_percent"
  else
    emit disk.collector_available false
    emit disk.total_kib '<unavailable>'
    emit disk.used_kib '<unavailable>'
    emit disk.available_kib '<unavailable>'
    emit disk.use_percent '<unavailable>'
    mark_incomplete
  fi
else
  emit disk.collector_available false
  emit disk.total_kib '<unavailable>'
  emit disk.used_kib '<unavailable>'
  emit disk.available_kib '<unavailable>'
  emit disk.use_percent '<unavailable>'
  mark_incomplete
fi

if command -v systemctl >/dev/null 2>&1; then
  if service_output="$(systemctl show \
    --no-pager \
    --property=LoadState,ActiveState,SubState,MainPID,FragmentPath,UnitFileState \
    "$service_name" 2>/dev/null)"; then
    service_properties_seen='|'
    while IFS='=' read -r property value; do
      case "$property" in
        LoadState|ActiveState|SubState|MainPID|FragmentPath|UnitFileState)
          emit "service.$property" "$value"
          service_properties_seen="${service_properties_seen}${property}|"
          ;;
      esac
    done <<< "$service_output"

    service_output_complete=true
    for required_property in \
      LoadState ActiveState SubState MainPID FragmentPath UnitFileState; do
      case "$service_properties_seen" in
        *"|$required_property|"*) ;;
        *) service_output_complete=false ;;
      esac
    done
    if [ "$service_output_complete" = true ]; then
      emit service.collector_available true
    else
      emit service.collector_available false
      mark_incomplete
    fi
  else
    emit service.collector_available false
    mark_incomplete
  fi
else
  emit service.collector_available false
  mark_incomplete
fi

if command -v docker >/dev/null 2>&1; then
  if container_output="$(docker ps -a \
    --filter 'name=vidlens' \
    --format '{{.Names}}|{{.Image}}|{{.Status}}' 2>/dev/null)"; then
    emit docker.collector_available true
    container_index=0
    while IFS= read -r container_line; do
      [ -n "$container_line" ] || continue
      container_index=$((container_index + 1))
      emit "container.$container_index" "$container_line"
    done <<< "$container_output"
    emit container.count "$container_index"
  else
    emit docker.collector_available false
    mark_incomplete
  fi
else
  emit docker.collector_available false
  mark_incomplete
fi

ports=(18083 3306 5432 5433 19530 9092 6379 9000)
if command -v ss >/dev/null 2>&1; then
  if listener_output="$(ss -H -lnt 2>/dev/null)"; then
    emit network.listener_collector_available true
    for port in "${ports[@]}"; do
      count="$(awk -v suffix=":$port" '$4 ~ (suffix "$") { total++ } END { print total + 0 }' <<< "$listener_output")"
      emit "listener.$port" "$count"
    done
  else
    emit network.listener_collector_available false
    mark_incomplete
  fi

  if connection_output="$(ss -H -nt state established 2>/dev/null)"; then
    emit network.connection_collector_available true
    for port in "${ports[@]}"; do
      count="$(awk -v suffix=":$port" '($4 ~ (suffix "$") || $5 ~ (suffix "$")) { total++ } END { print total + 0 }' <<< "$connection_output")"
      emit "connection.$port" "$count"
    done
  else
    emit network.connection_collector_available false
    mark_incomplete
  fi
else
  emit network.listener_collector_available false
  emit network.connection_collector_available false
  mark_incomplete
fi

endpoint_evidence() {
  local label="$1"
  local path="$2"
  local result
  if result="$(curl -sS -o /dev/null -w '%{http_code}|%{content_type}' \
    --connect-timeout 2 --max-time 5 "$local_base_url$path" 2>/dev/null)"; then
    http_code="${result%%|*}"
    content_type="${result#*|}"
    [[ "$http_code" =~ ^[0-9]{3}$ ]] || http_code='<invalid>'
    [ -n "$content_type" ] || content_type='<empty>'
    emit "endpoint.$label.http_code" "$http_code"
    emit "endpoint.$label.content_type" "$content_type"
  else
    emit "endpoint.$label.http_code" 000
    emit "endpoint.$label.content_type" '<unavailable>'
  fi
}

if command -v curl >/dev/null 2>&1; then
  emit endpoint.collector_available true
  endpoint_evidence health /health
  endpoint_evidence healthz /healthz
  endpoint_evidence readyz /readyz
else
  emit endpoint.collector_available false
  mark_incomplete
fi

emit audit.collection_complete "$collection_complete"
[ "$collection_complete" = true ] || exit 2
