"""Offline model installation identity. Hash weights only on explicit prepare.

Runtime validation checks the prepared file inventory and stat signatures. Any
local replacement requires preparing a new manifest before inference/cache use.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile

MANIFEST = 'vidlens-model-manifest.json'
SCHEMA = 'alignment-model-v1'


class ModelIdentityError(ValueError):
    pass


def inventory(root):
    return sorted(p for p in root.rglob('*') if p.is_file() and p.name != MANIFEST
                  and '.cache' not in p.relative_to(root).parts and p.suffix in
                  {'.safetensors', '.bin', '.json', '.txt', '.model'})


def signature(path):
    s = path.stat()
    return [s.st_size, s.st_mtime_ns, s.st_ctime_ns, s.st_ino]


def atomic_json(path, value):
    fd, temp = tempfile.mkstemp(dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as f:
            json.dump(value, f, ensure_ascii=False, sort_keys=True)
            f.flush()
            os.fsync(f.fileno())
        os.replace(temp, path)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


def prepare(root, revision, backend):
    root = Path(root)
    if not re.fullmatch(r'[0-9a-f]{40,64}', revision or ''):
        raise ModelIdentityError('model_revision_required')
    paths = inventory(root)
    if not (root / 'config.json').is_file() or not any(p.suffix in {'.safetensors', '.bin'} for p in paths):
        raise ModelIdentityError('model_missing')
    files = {}
    for path in paths:
        before = signature(path)
        h = hashlib.sha256()
        with path.open('rb') as f:
            for block in iter(lambda: f.read(1024 * 1024), b''):
                h.update(block)
        if signature(path) != before:
            raise ModelIdentityError('model_manifest_stale')
        files[path.relative_to(root).as_posix()] = {'signature': before, 'sha256': h.hexdigest()}
    identity = hashlib.sha256(json.dumps([SCHEMA, backend, revision,
        [(name, v['signature'][0], v['sha256']) for name, v in files.items()]], sort_keys=True).encode()).hexdigest()
    manifest = {'schema': SCHEMA, 'backend': backend, 'revision': revision, 'identity': identity, 'files': files}
    atomic_json(root / MANIFEST, manifest)
    return manifest


def validate(root, backend, revision=None):
    root = Path(root or '')
    if not root.is_dir() or not (root / 'config.json').is_file():
        raise ModelIdentityError('model_missing')
    try:
        manifest = json.loads((root / MANIFEST).read_text(encoding='utf-8'))
    except (OSError, ValueError):
        raise ModelIdentityError('model_manifest_missing') from None
    if manifest.get('schema') != SCHEMA or manifest.get('backend') != backend:
        raise ModelIdentityError('model_manifest_stale')
    if not re.fullmatch(r'[0-9a-f]{40,64}', manifest.get('revision', '')) or revision and manifest['revision'] != revision:
        raise ModelIdentityError('model_manifest_stale')
    files = manifest.get('files', {})
    actual = {p.relative_to(root).as_posix(): signature(p) for p in inventory(root)}
    if not files or actual != {name: value.get('signature') for name, value in files.items()}:
        raise ModelIdentityError('model_manifest_stale')
    expected = hashlib.sha256(json.dumps([SCHEMA, backend, manifest['revision'],
        [(name, v['signature'][0], v['sha256']) for name, v in sorted(files.items())]], sort_keys=True).encode()).hexdigest()
    if manifest.get('identity') != expected:
        raise ModelIdentityError('model_manifest_stale')
    return manifest
