#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
audit_script="$repo_root/deploy/server-preflight-audit.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/vidlens-server-audit-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_line() {
  local path="$1"
  local expected="$2"
  grep -Fqx "$expected" "$path" || fail "missing line '$expected' in $path"
}

new_case() {
  local name="$1"
  case_root="$test_root/$name"
  deploy_dir="$case_root/deploy"
  stub_dir="$case_root/bin"
  call_log="$case_root/calls.log"
  output_file="$case_root/audit.out"

  mkdir -p \
    "$deploy_dir/web/dist" \
    "$deploy_dir/data/mysql" \
    "$deploy_dir/data/postgres" \
    "$stub_dir"
  printf 'server-binary\n' > "$deploy_dir/server"
  printf 'database:\n  password: top-secret-password\n' > "$deploy_dir/config.yaml"
  printf 'old-web\n' > "$deploy_dir/web/dist/index.html"
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  printf 'mysql-data\n' > "$deploy_dir/data/mysql/table.dat"
  printf 'postgres-data\n' > "$deploy_dir/data/postgres/table.dat"

  cat > "$stub_dir/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "$*" >> "$CALL_LOG"
[ "${1:-}" = "show" ] || exit 64
cat <<'OUTPUT'
LoadState=loaded
ActiveState=active
SubState=running
MainPID=4242
FragmentPath=/etc/systemd/system/vidlens.service
UnitFileState=enabled
OUTPUT
STUB

  cat > "$stub_dir/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >> "$CALL_LOG"
[ "${1:-}" = "ps" ] || exit 64
cat <<'OUTPUT'
vidlens-mysql|mysql:8.0|Exited (0) 1 hour ago
vidlens-postgres|pgvector/pgvector:pg17|Up 1 hour (healthy)
vidlens-milvus|milvusdb/milvus:v2.4.15|Exited (0) 1 hour ago
OUTPUT
STUB

  cat > "$stub_dir/ss" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'ss %s\n' "$*" >> "$CALL_LOG"
case " $* " in
  *" -lnt "*)
    cat <<'OUTPUT'
LISTEN 0 4096 0.0.0.0:3306 0.0.0.0:*
LISTEN 0 4096 127.0.0.1:5432 0.0.0.0:*
LISTEN 0 4096 127.0.0.1:18083 0.0.0.0:*
OUTPUT
    ;;
  *)
    cat <<'OUTPUT'
ESTAB 0 0 127.0.0.1:42000 127.0.0.1:3306
ESTAB 0 0 127.0.0.1:18083 127.0.0.1:42001
OUTPUT
    ;;
esac
STUB

  cat > "$stub_dir/df" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'df %s\n' "$*" >> "$CALL_LOG"
cat <<'OUTPUT'
Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/fake 100000 40000 60000 40% /srv
OUTPUT
STUB

  cat > "$stub_dir/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'curl %s\n' "$*" >> "$CALL_LOG"
url="${!#}"
case "$url" in
  */health) printf '200|application/json' ;;
  */healthz) printf '200|application/json' ;;
  */readyz) printf '404|text/html' ;;
  *) exit 22 ;;
esac
STUB
  chmod +x "$stub_dir/systemctl" "$stub_dir/docker" "$stub_dir/ss" "$stub_dir/df" "$stub_dir/curl"
}

run_audit() {
  PATH="$stub_dir:$PATH" \
  CALL_LOG="$call_log" \
  DEPLOY_PATH="$deploy_dir" \
  DATA_ROOT="$deploy_dir/data" \
  SERVICE_NAME="vidlens.service" \
  LOCAL_BASE_URL="http://127.0.0.1:18083" \
  bash "$audit_script" > "$output_file"
}

test_collects_only_non_secret_runtime_evidence() {
  new_case evidence
  run_audit || fail 'audit should complete with available collectors'

  assert_line "$output_file" 'audit.version=1'
  assert_line "$output_file" "deploy.path=$deploy_dir"
  assert_line "$output_file" 'runtime.marker.exists=true'
  assert_line "$output_file" 'runtime.marker.value=postgres-pgvector-v1'
  assert_line "$output_file" 'artifact.server.exists=true'
  assert_line "$output_file" "artifact.server.sha256=$(sha256sum "$deploy_dir/server" | awk '{ print $1 }')"
  assert_line "$output_file" 'artifact.config.exists=true'
  assert_line "$output_file" "artifact.config.sha256=$(sha256sum "$deploy_dir/config.yaml" | awk '{ print $1 }')"
  assert_line "$output_file" 'artifact.web_dist.exists=true'
  assert_line "$output_file" 'data.mysql.exists=true'
  assert_line "$output_file" 'data.postgres.exists=true'
  assert_line "$output_file" 'data.redis.exists=false'
  assert_line "$output_file" 'data.minio.exists=false'
  assert_line "$output_file" 'disk.total_kib=100000'
  assert_line "$output_file" 'disk.used_kib=40000'
  assert_line "$output_file" 'disk.available_kib=60000'
  assert_line "$output_file" 'disk.use_percent=40%'
  assert_line "$output_file" 'service.ActiveState=active'
  assert_line "$output_file" 'service.MainPID=4242'
  assert_line "$output_file" 'container.1=vidlens-mysql|mysql:8.0|Exited (0) 1 hour ago'
  assert_line "$output_file" 'listener.3306=1'
  assert_line "$output_file" 'listener.5432=1'
  assert_line "$output_file" 'listener.18083=1'
  assert_line "$output_file" 'connection.3306=1'
  assert_line "$output_file" 'endpoint.health.http_code=200'
  assert_line "$output_file" 'endpoint.health.content_type=application/json'
  assert_line "$output_file" 'endpoint.readyz.http_code=404'
  assert_line "$output_file" 'endpoint.readyz.content_type=text/html'
  assert_line "$output_file" 'audit.collection_complete=true'

  if grep -Fq 'top-secret-password' "$output_file"; then
    fail 'audit leaked config content'
  fi
  [ "$(grep -c '^systemctl show ' "$call_log")" -eq 1 ] \
    || fail 'audit must only issue one read-only systemctl show call'
  [ "$(grep -c '^docker ps ' "$call_log")" -eq 1 ] \
    || fail 'audit must only issue one read-only docker ps call'
}

test_missing_marker_is_reported_without_failure() {
  new_case missing-marker
  rm -f "$deploy_dir/.runtime-generation"
  run_audit || fail 'missing migration marker is an audit finding, not a collector failure'

  assert_line "$output_file" 'runtime.marker.exists=false'
  assert_line "$output_file" 'runtime.marker.value=<missing>'
  assert_line "$output_file" 'audit.collection_complete=true'
}

test_unsafe_multiline_marker_is_redacted() {
  new_case unsafe-marker
  printf 'postgres-pgvector-v1\nuntrusted-extra-line\n' > "$deploy_dir/.runtime-generation"
  run_audit || fail 'unsafe marker should be reported without aborting collection'

  assert_line "$output_file" 'runtime.marker.exists=true'
  assert_line "$output_file" 'runtime.marker.value=<invalid>'
  if grep -Fq 'untrusted-extra-line' "$output_file"; then
    fail 'audit emitted unvalidated marker content'
  fi
}

test_required_collector_failures_return_exit_two() {
  new_case collector-failure
  cat > "$stub_dir/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 70
STUB
  cat > "$stub_dir/docker" <<'STUB'
#!/usr/bin/env bash
exit 71
STUB
  chmod +x "$stub_dir/systemctl" "$stub_dir/docker"

  set +e
  run_audit
  status=$?
  set -e

  [ "$status" -eq 2 ] || fail "collector failure must exit 2, got $status"
  assert_line "$output_file" 'service.collector_available=false'
  assert_line "$output_file" 'docker.collector_available=false'
  assert_line "$output_file" 'audit.collection_complete=false'
  assert_line "$output_file" 'endpoint.health.http_code=200'
}

test_invalid_disk_evidence_returns_exit_two() {
  new_case invalid-disk
  cat > "$stub_dir/df" <<'STUB'
#!/usr/bin/env bash
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf '/dev/fake unknown unknown unknown unknown /srv\n'
STUB
  chmod +x "$stub_dir/df"

  set +e
  run_audit
  status=$?
  set -e

  [ "$status" -eq 2 ] || fail "invalid disk evidence must exit 2, got $status"
  assert_line "$output_file" 'disk.collector_available=false'
  assert_line "$output_file" 'disk.total_kib=<unavailable>'
  assert_line "$output_file" 'audit.collection_complete=false'
}

test_incomplete_systemd_evidence_returns_exit_two() {
  new_case incomplete-systemd
  cat > "$stub_dir/systemctl" <<'STUB'
#!/usr/bin/env bash
printf 'ActiveState=active\n'
STUB
  chmod +x "$stub_dir/systemctl"

  set +e
  run_audit
  status=$?
  set -e

  [ "$status" -eq 2 ] || fail "incomplete systemd evidence must exit 2, got $status"
  assert_line "$output_file" 'service.ActiveState=active'
  assert_line "$output_file" 'service.collector_available=false'
  assert_line "$output_file" 'audit.collection_complete=false'
}

[ -f "$audit_script" ] || fail "audit script does not exist: $audit_script"
test_collects_only_non_secret_runtime_evidence
test_missing_marker_is_reported_without_failure
test_unsafe_multiline_marker_is_redacted
test_required_collector_failures_return_exit_two
test_invalid_disk_evidence_returns_exit_two
test_incomplete_systemd_evidence_returns_exit_two
printf 'PASS: server preflight audit tests\n'
