#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy_script="$repo_root/deploy/web-next-deploy.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/vidlens-web-next-deploy-test.XXXXXX")"
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

assert_exists() {
  [ -e "$1" ] || fail "missing path: $1"
}

assert_no_old_release_left() {
  local leftovers
  leftovers="$(find "$deploy_dir" -maxdepth 1 -name 'web-next.old-*' -o -name 'web-next.failed-*' -o -name 'web-next.new-*' 2>/dev/null)"
  [ -z "$leftovers" ] || fail "leftover staging paths: $leftovers"
}

new_case() {
  local name="$1"
  case_root="$test_root/$name"
  deploy_dir="$case_root/deploy"
  artifact_dir="$case_root/artifacts"
  stub_dir="$case_root/bin"
  call_log="$case_root/calls.log"
  sha="0123456789abcdef0123456789abcdef01234567"

  mkdir -p "$deploy_dir/web-next/.next" "$artifact_dir/web-next-src" "$stub_dir"
  printf '{"name":"old-web-next"}\n' > "$deploy_dir/web-next/package.json"
  printf 'old-build\n' > "$deploy_dir/web-next/.next/BUILD_ID"

  printf '{"fake":true}\n' > "$artifact_dir/web-next-src/package.json"
  tar -czf "$artifact_dir/web-next-src.tar.gz" -C "$artifact_dir/web-next-src" .

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
exit 0
STUB
  cat > "$stub_dir/npm" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'npm %s\n' "$*" >> "$CALL_LOG"
if [ "${1:-}" = "run" ] && [ "${2:-}" = "build" ]; then
  if [ "${FAIL_BUILD:-0}" = "1" ]; then
    exit 1
  fi
  mkdir -p .next
  printf 'new-build\n' > .next/BUILD_ID
fi
exit 0
STUB
  chmod +x "$stub_dir/systemctl" "$stub_dir/curl" "$stub_dir/npm"
}

run_deploy() {
  PATH="$stub_dir:$PATH" \
  CALL_LOG="$call_log" \
  DEPLOY_PATH="$deploy_dir" \
  DEPLOY_TMP_DIR="$artifact_dir" \
  GITHUB_SHA="$sha" \
  VIDLENS_API_BASE="http://127.0.0.1:18083" \
  WEB_NEXT_HEALTH_URL="http://127.0.0.1:18084/" \
  bash "$deploy_script"
}

test_missing_artifact_is_rejected() {
  new_case missing-artifact
  rm -f "$artifact_dir/web-next-src.tar.gz"

  if run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly accepted a missing web-next artifact'
  fi

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" old-build
  assert_not_exists "$call_log"
}

test_failed_build_keeps_old_release() {
  new_case failed-build

  if FAIL_BUILD=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded with a failing build'
  fi

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" old-build
  assert_exists "$deploy_dir/web-next"
  assert_no_old_release_left
}

test_successful_deploy_swaps_and_restarts() {
  new_case success

  run_deploy >"$case_root/stdout" 2>"$case_root/stderr"

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" new-build
  grep -q "systemctl restart vidlens-web" "$call_log" || fail 'vidlens-web was not restarted'
  grep -q "curl" "$call_log" || fail 'health check was not performed'
  assert_no_old_release_left
}

test_restart_failure_rolls_back() {
  new_case restart-failure

  if FAIL_FIRST_RESTART=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded with a failing restart'
  fi

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" old-build
  assert_no_old_release_left
}

test_health_failure_rolls_back() {
  new_case health-failure

  if FAIL_HEALTH=1 run_deploy >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded with a failing health check'
  fi

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" old-build
  assert_no_old_release_left
}

test_missing_vidlens_api_base_is_rejected() {
  new_case missing-env

  if env -u VIDLENS_API_BASE \
    PATH="$stub_dir:$PATH" \
    CALL_LOG="$call_log" \
    DEPLOY_PATH="$deploy_dir" \
    DEPLOY_TMP_DIR="$artifact_dir" \
    GITHUB_SHA="$sha" \
    WEB_NEXT_HEALTH_URL="http://127.0.0.1:18084/" \
    bash "$deploy_script" >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail 'deployment unexpectedly succeeded without VIDLENS_API_BASE'
  fi

  assert_file_text "$deploy_dir/web-next/.next/BUILD_ID" old-build
}

test_missing_artifact_is_rejected
test_failed_build_keeps_old_release
test_successful_deploy_swaps_and_restarts
test_restart_failure_rolls_back
test_health_failure_rolls_back
test_missing_vidlens_api_base_is_rejected

printf 'web-next-deploy tests passed\n'