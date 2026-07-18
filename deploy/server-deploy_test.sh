#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy_script="$repo_root/deploy/server-deploy.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/vidlens-server-deploy-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_file_text() {
  local path="$1"
  local expected="$2"
  [ -f "$path" ] || fail "missing file: $path"
  local actual
  actual="$(cat "$path")"
  [ "$actual" = "$expected" ] || fail "$path contains '$actual', want '$expected'"
}

assert_not_exists() {
  [ ! -e "$1" ] || fail "path should not exist: $1"
}

new_case() {
  local name="$1"
  case_root="$test_root/$name"
  deploy_dir="$case_root/deploy"
  artifact_dir="$case_root/artifacts"
  stub_dir="$case_root/bin"
  call_log="$case_root/calls.log"
  sha="0123456789abcdef0123456789abcdef01234567"

  mkdir -p "$deploy_dir/web/dist" "$artifact_dir/web-source/dist" "$stub_dir"
  printf 'old-server\n' > "$deploy_dir/server"
  printf 'old-web\n' > "$deploy_dir/web/dist/index.html"
  printf 'new-server\n' > "$artifact_dir/server"
  printf 'new-web\n' > "$artifact_dir/web-source/dist/index.html"
  tar -czf "$artifact_dir/web-dist.tar.gz" -C "$artifact_dir/web-source" dist

  cat > "$stub_dir/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "$*" >> "$CALL_LOG"
if [ "${FAIL_FIRST_RESTART:-0}" = "1" ] && [ "${1:-}" = "restart" ]; then
  failure_marker="${CALL_LOG}.restart-failed"
  if [ ! -e "$failure_marker" ]; then
    : > "$failure_marker"
    exit 1
  fi
fi
exit 0
STUB
  cat > "$stub_dir/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'curl %s\n' "$*" >> "$CALL_LOG"
if [ "${FAIL_HEALTH:-0}" = "1" ]; then
  exit 22
fi
printf '{"status":"ok"}\n'
STUB
  chmod +x "$stub_dir/systemctl" "$stub_dir/curl"
}

run_deploy() {
  PATH="$stub_dir:$PATH" \
  CALL_LOG="$call_log" \
  DEPLOY_PATH="${TEST_DEPLOY_PATH:-$deploy_dir}" \
  DEPLOY_TMP_DIR="${TEST_ARTIFACT_DIR:-$artifact_dir}" \
  GITHUB_SHA="$sha" \
  EXPECTED_RUNTIME_GENERATION="postgres-pgvector-v1" \
  HEALTH_URL="http://127.0.0.1:18083/readyz" \
  bash "$deploy_script"
}

test_runtime_marker_with_embedded_newline_is_rejected() {
  new_case marker-newline
  printf 'postgres-pgvector-\nv1\n' > "$deploy_dir/.runtime-generation"

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted a split runtime marker'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
}

test_path_with_parent_segment_is_rejected() {
  new_case path-parent-segment
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  mkdir -p "$case_root/path-segment"

  if TEST_DEPLOY_PATH="$case_root/path-segment/../deploy" \
    run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted a path containing a parent segment'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
}

test_artifact_directory_nested_under_deploy_is_rejected() {
  new_case nested-artifacts
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  local nested_artifacts="$deploy_dir/uploaded-artifacts"
  mv "$artifact_dir" "$nested_artifacts"
  artifact_dir="$nested_artifacts"

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted artifacts nested under the deploy directory'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
}

test_missing_runtime_marker_blocks_before_mutation() {
  new_case marker-block

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded without runtime marker'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
  [ -z "$(find "$deploy_dir/.logs/deploy-backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null || true)" ] \
    || fail 'marker rejection must not create a release backup'
}

test_runtime_marker_mismatch_blocks_before_mutation() {
  new_case marker-mismatch
  printf 'mysql-milvus-v1\n' > "$deploy_dir/.runtime-generation"

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted a mismatched runtime marker'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
  grep -q 'runtime migration marker mismatch' "$case_root/stderr" \
    || fail 'marker mismatch should be explicit'
}

test_web_artifact_without_index_is_rejected_before_activation() {
  new_case invalid-web-artifact
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  rm -f "$artifact_dir/web-source/dist/index.html" "$artifact_dir/web-dist.tar.gz"
  printf 'asset\n' > "$artifact_dir/web-source/dist/app.js"
  tar -czf "$artifact_dir/web-dist.tar.gz" -C "$artifact_dir/web-source" dist

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted a web artifact without index.html'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  assert_not_exists "$call_log"
}

test_success_activates_release_and_keeps_backup() {
  new_case success
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"

  run_deploy >"$case_root/stdout" 2>"$case_root/stderr" \
    || { cat "$case_root/stderr" >&2; fail 'deployment should succeed'; }

  assert_file_text "$deploy_dir/server" new-server
  assert_file_text "$deploy_dir/web/dist/index.html" new-web
  assert_not_exists "$artifact_dir"

  local backup_dir
  backup_dir="$(find "$deploy_dir/.logs/deploy-backups" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  [ -n "$backup_dir" ] || fail 'success backup directory missing'
  assert_file_text "$backup_dir/server" old-server
  [ -s "$backup_dir/web-dist.tar.gz" ] || fail 'success web backup missing'
  [ "$(grep -c '^systemctl restart vidlens.service$' "$call_log")" -eq 1 ] \
    || fail 'success should restart service exactly once'
}

test_success_backs_up_config_without_modifying_it() {
  new_case config-backup
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  printf 'database: local-test\n' > "$deploy_dir/config.yaml"

  run_deploy >"$case_root/stdout" 2>"$case_root/stderr" \
    || { cat "$case_root/stderr" >&2; fail 'deployment with config should succeed'; }

  assert_file_text "$deploy_dir/config.yaml" 'database: local-test'
  local backup_dir
  backup_dir="$(find "$deploy_dir/.logs/deploy-backups" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  assert_file_text "$backup_dir/config.yaml" 'database: local-test'
}

test_restart_failure_restores_previous_release() {
  new_case restart-rollback
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"

  if FAIL_FIRST_RESTART=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded when the first restart failed'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  [ "$(grep -c '^systemctl restart vidlens.service$' "$call_log")" -eq 2 ] \
    || fail 'restart failure should trigger one rollback restart'
  if grep -q '^curl ' "$call_log"; then
    fail 'readiness check must not run after restart failure'
  fi
}

test_health_failure_restores_previous_release() {
  new_case rollback
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"

  if FAIL_HEALTH=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded when health check failed'
  fi

  assert_file_text "$deploy_dir/server" old-server
  assert_file_text "$deploy_dir/web/dist/index.html" old-web
  [ -d "$artifact_dir" ] || fail 'failed deployment should retain uploaded artifacts for diagnosis'
  [ "$(grep -c '^systemctl restart vidlens.service$' "$call_log")" -eq 2 ] \
    || fail 'rollback should restart once for deploy and once after restore'
  grep -q 'Rollback restored previous release' "$case_root/stderr" \
    || fail 'rollback result should be explicit in stderr'
}

test_first_deploy_health_failure_removes_release_and_stops_service() {
  new_case first-deploy-rollback
  printf 'postgres-pgvector-v1\n' > "$deploy_dir/.runtime-generation"
  rm -f "$deploy_dir/server"
  rm -rf "$deploy_dir/web/dist"

  if FAIL_HEALTH=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'first deployment unexpectedly succeeded when health check failed'
  fi

  assert_not_exists "$deploy_dir/server"
  assert_not_exists "$deploy_dir/web/dist"
  [ "$(grep -c '^systemctl restart vidlens.service$' "$call_log")" -eq 1 ] \
    || fail 'first deployment should attempt one service restart'
  [ "$(grep -c '^systemctl stop vidlens.service$' "$call_log")" -eq 1 ] \
    || fail 'failed first deployment should stop the newly started service'
}

[ -f "$deploy_script" ] || fail "deployment script does not exist: $deploy_script"
test_missing_runtime_marker_blocks_before_mutation
test_runtime_marker_with_embedded_newline_is_rejected
test_path_with_parent_segment_is_rejected
test_artifact_directory_nested_under_deploy_is_rejected
test_runtime_marker_mismatch_blocks_before_mutation
test_web_artifact_without_index_is_rejected_before_activation
test_success_activates_release_and_keeps_backup
test_success_backs_up_config_without_modifying_it
test_restart_failure_restores_previous_release
test_health_failure_restores_previous_release
test_first_deploy_health_failure_removes_release_and_stops_service
printf 'PASS: server deployment safety tests\n'