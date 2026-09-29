#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
output="${1:?release output directory is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
[[ "$GITHUB_SHA" =~ ^[0-9a-f]{40}$ ]] || { echo 'Invalid release commit' >&2; exit 1; }
[ "$(git rev-parse HEAD)" = "$GITHUB_SHA" ] || { echo 'Release commit differs from checkout' >&2; exit 1; }
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo 'Tracked checkout changes cannot be released' >&2; exit 1; }
[ -f frontend/dist/index.html ] || { echo 'Frontend must be built before packaging' >&2; exit 1; }

mkdir -p "$output"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$output/server" ./cmd/server
tar -czf "$output/frontend-build.tar.gz" -C frontend dist server.mjs package.json package-lock.json
python3 - "$output" "$GITHUB_SHA" <<'PY'
import hashlib
import json
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
files = {}
for name in ('server', 'frontend-build.tar.gz'):
    path = output / name
    files[name] = {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size': path.stat().st_size}
manifest = {'schema': 1, 'sha': sys.argv[2], 'os': 'linux', 'arch': 'amd64',
            'runtime_generation': 'postgres-pgvector-v1', 'files': files}
(output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
PY
