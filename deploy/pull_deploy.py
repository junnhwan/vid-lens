#!/usr/bin/env python3
"""Pull checked public releases; all production settings remain on this server."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import signal
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
from urllib.parse import urlparse


SHA = re.compile(r'[0-9a-f]{40}')
REPOSITORY = re.compile(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+')
ASSETS = {'server': 128 * 1024**2, 'frontend-build.tar.gz': 256 * 1024**2}


class DeployError(Exception):
    pass


class ActivationError(DeployError):
    pass


def log(message):
    print(message, flush=True)


def read_json(path):
    if path.stat().st_size > 16384:
        raise DeployError('JSON file exceeds the size limit')
    value = json.loads(path.read_text(encoding='utf-8'))
    if not isinstance(value, dict):
        raise DeployError('Expected a JSON object')
    return value


def write_state(path, state):
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=path.parent, delete=False) as file:
        json.dump(state, file)
        file.write('\n')
        temporary = Path(file.name)
    temporary.chmod(0o600)
    os.replace(temporary, path)


def load_config(path):
    config = read_json(path)
    if not REPOSITORY.fullmatch(config.get('repository', '')) or config.get('branch') != 'main':
        raise DeployError('Configure a GitHub repository and the main branch')
    for name in ('deployment_path', 'state_dir', 'scripts_dir'):
        raw = config.get(name, '')
        candidate = Path(raw)
        if not candidate.is_absolute() or '..' in candidate.parts or str(candidate) in ('/', '/tmp'):
            raise DeployError('Invalid local directory setting: ' + name)
        if candidate.is_symlink() or candidate.resolve() != candidate:
            raise DeployError('Local directories must be normalized and cannot be symlinks')
    deploy, state = Path(config['deployment_path']), Path(config['state_dir'])
    if deploy == state or deploy in state.parents or state in deploy.parents:
        raise DeployError('Deployment and state directories must be separate')
    if not re.fullmatch(r'[a-z0-9][a-z0-9._-]{0,63}', config.get('runtime_generation', '')):
        raise DeployError('Invalid runtime generation')
    for name in ('api_base', 'backend_health_url', 'frontend_health_url'):
        url = urlparse(config.get(name, ''))
        if url.scheme != 'http' or url.hostname not in ('127.0.0.1', 'localhost', '::1') or url.username or url.password:
            raise DeployError('Runtime and health URLs must use loopback HTTP')
    if not re.fullmatch(r'[A-Za-z0-9_.@-]+', config.get('backend_service', 'vidlens.service')):
        raise DeployError('Invalid backend service name')
    proxy = config.get('https_proxy', '')
    if proxy:
        parsed = urlparse(proxy)
        if parsed.scheme not in ('http', 'https') or not parsed.hostname:
            raise DeployError('Invalid server-local HTTPS proxy setting')
    return config


def open_public(url, proxy=None):
    request = urllib.request.Request(url, headers={'User-Agent': 'VidLens-Pull-Deploy/1'})
    routes = [{}] + ([{'https': proxy}] if proxy else [])
    for index, route in enumerate(routes):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler(route))
        try:
            return opener.open(request, timeout=45)
        except urllib.error.HTTPError:
            raise
        except (urllib.error.URLError, TimeoutError, OSError):
            if index == len(routes) - 1:
                raise DeployError('GitHub HTTPS request failed; retrying on the next check') from None


def main_sha(config):
    # Public REST avoids the Git smart-HTTP transport that intermittently hung
    # in the production timer. At two-minute intervals, idle polling is 30/h.
    url = 'https://api.github.com/repos/' + config['repository'] + '/git/ref/heads/main'
    with open_public(url, proxy=config.get('https_proxy')) as response:
        body = response.read(16385)
    if len(body) > 16384:
        raise DeployError('GitHub reference response exceeds the size limit')
    reference = json.loads(body)
    obj = reference.get('object', {}) if isinstance(reference, dict) else {}
    sha = obj.get('sha', '') if isinstance(obj, dict) else ''
    if reference.get('ref') != 'refs/heads/main' or obj.get('type') != 'commit' or not SHA.fullmatch(sha):
        raise DeployError('Could not identify the main commit')
    return sha


def download(url, path, limit, missing_ok=False, proxy=None):
    try:
        with open_public(url, proxy=proxy) as response, path.open('wb') as output:
            size = 0
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                size += len(block)
                if size > limit:
                    raise DeployError('Release asset exceeds the size limit')
                output.write(block)
    except urllib.error.HTTPError as error:
        if missing_ok and error.code == 404:
            return False
        raise DeployError('GitHub asset download failed (HTTP %s)' % error.code) from None
    return True


def validate_manifest(manifest, sha, generation):
    if (manifest.get('schema') != 1 or manifest.get('sha') != sha or manifest.get('os') != 'linux'
            or manifest.get('arch') != 'amd64' or manifest.get('runtime_generation') != generation):
        raise DeployError('Release identity or runtime generation mismatch')
    files = manifest.get('files')
    if not isinstance(files, dict) or set(files) != set(ASSETS):
        raise DeployError('Release asset list mismatch')
    for name, limit in ASSETS.items():
        metadata = files[name]
        if not isinstance(metadata, dict) or not re.fullmatch(r'[0-9a-f]{64}', metadata.get('sha256', '')):
            raise DeployError('Invalid asset digest')
        size = metadata.get('size')
        if type(size) is not int or not 0 < size <= limit:
            raise DeployError('Invalid asset size')


def verify_file(path, metadata):
    digest = hashlib.sha256()
    with path.open('rb') as file:
        for block in iter(lambda: file.read(1024 * 1024), b''):
            digest.update(block)
    if path.stat().st_size != metadata['size'] or digest.hexdigest() != metadata['sha256']:
        raise DeployError('Release asset checksum mismatch')


def validate_frontend(path):
    seen, size = set(), 0
    with tarfile.open(path, 'r:gz') as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or not name.parts:
                raise DeployError('Unsafe frontend archive path')
            if not member.isfile() and not member.isdir():
                raise DeployError('Frontend archive must not contain links or special files')
            if name.parts[0] != 'dist' and str(name) not in ('server.mjs', 'package.json', 'package-lock.json'):
                raise DeployError('Unexpected file in frontend archive')
            if any(part.startswith('.env') for part in name.parts) or str(name) in seen:
                raise DeployError('Private or duplicate file in frontend archive')
            seen.add(str(name))
            size += member.size
            if size > 512 * 1024**2:
                raise DeployError('Expanded frontend exceeds the size limit')
    if not {'dist/index.html', 'server.mjs', 'package.json', 'package-lock.json'} <= seen:
        raise DeployError('Frontend runtime files are missing')


def command(arguments, environment=None):
    # Capture local script output so runtime URLs/configuration are not propagated.
    process = subprocess.Popen(arguments, env=environment, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, start_new_session=True)
    try:
        process.communicate(timeout=180)
    except BaseException:
        # Stop the shell and its children before restoring any live program files.
        try:
            os.killpg(process.pid, signal.SIGTERM)
            process.communicate(timeout=5)
        except (ProcessLookupError, subprocess.TimeoutExpired):
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate()
        raise
    if process.returncode:
        raise DeployError('Local deployment command failed: ' + Path(arguments[0]).name)


def health(config):
    command(['systemctl', 'is-active', '--quiet', config.get('backend_service', 'vidlens.service'), 'vidlens-web'])
    for name in ('backend_health_url', 'frontend_health_url'):
        command(['curl', '-fsS', '--max-time', '15', '--retry', '5', '--retry-delay', '2',
                 '--retry-connrefused', config[name]])


def snapshot(config, stamp):
    deploy = Path(config['deployment_path'])
    if not (deploy / 'server').is_file() or not (deploy / 'frontend').is_dir():
        raise DeployError('Automatic deployment requires an existing backend and frontend')
    backup = deploy / '.logs' / 'auto-deploy-backups' / stamp
    backup.mkdir(parents=True, mode=0o700)
    shutil.copy2(deploy / 'server', backup / 'server')
    shutil.copytree(deploy / 'frontend', backup / 'frontend', symlinks=True)
    return backup


def restore(config, backup, stamp):
    deploy = Path(config['deployment_path'])
    staged_server = deploy / ('server.restore-' + stamp)
    shutil.copy2(backup / 'server', staged_server)
    os.replace(staged_server, deploy / 'server')
    staged_frontend = deploy / ('frontend.restore-' + stamp)
    shutil.copytree(backup / 'frontend', staged_frontend, symlinks=True)
    failed_frontend = deploy / ('frontend.failed-' + stamp)
    if (deploy / 'frontend').exists():
        os.replace(deploy / 'frontend', failed_frontend)
    os.replace(staged_frontend, deploy / 'frontend')
    command(['systemctl', 'restart', config.get('backend_service', 'vidlens.service'), 'vidlens-web'])
    health(config)
    if failed_frontend.exists():
        shutil.rmtree(failed_frontend)


def deploy_release(config, sha, work):
    health(config)
    deploy = Path(config['deployment_path'])
    marker = deploy / '.runtime-generation'
    if not marker.is_file() or marker.read_text().strip() != config['runtime_generation']:
        raise DeployError('Installed runtime generation mismatch')
    upload_marker = deploy / 'frontend' / '.upload-api-base'
    if upload_marker.is_file() and upload_marker.read_text().strip():
        raise DeployError('Prebuilt releases require same-origin uploads')
    stamp = 'auto-' + sha[:12] + '-' + str(time.time_ns())
    backup = snapshot(config, stamp)
    backend, frontend = work / 'backend', work / 'frontend'
    backend.mkdir()
    frontend.mkdir()
    shutil.copy2(work / 'server', backend / 'server')
    shutil.copy2(work / 'frontend-build.tar.gz', frontend / 'frontend-build.tar.gz')
    environment = dict(os.environ, DEPLOY_PATH=str(deploy), GITHUB_SHA=sha,
                       EXPECTED_RUNTIME_GENERATION=config['runtime_generation'],
                       SERVICE_NAME=config.get('backend_service', 'vidlens.service'),
                       HEALTH_URL=config['backend_health_url'])
    scripts = Path(config['scripts_dir'])
    try:
        log('Activating backend ' + sha[:12])
        command(['bash', str(scripts / 'server-deploy.sh')], dict(
            environment, DEPLOY_TMP_DIR=str(backend), DEPLOY_STAMP=stamp + '-backend'))
        log('Activating frontend ' + sha[:12])
        command(['bash', str(scripts / 'frontend-deploy.sh')], dict(
            environment, DEPLOY_TMP_DIR=str(frontend), DEPLOY_STAMP=stamp + '-frontend',
            FRONTEND_ARTIFACT_MODE='prebuilt', VIDLENS_UPLOAD_API_BASE='',
            VIDLENS_API_BASE=config['api_base'], FRONTEND_HEALTH_URL=config['frontend_health_url']))
        health(config)
    except BaseException as activation_error:
        log('Deployment failed; restoring both previous program releases')
        try:
            restore(config, backup, stamp)
        except BaseException as error:
            raise ActivationError('Rollback did not pass health checks; inspect the server') from error
        raise ActivationError('Program activation failed; previous programs restored and checked') from activation_error


def prune_backups(config, keep=5):
    deploy = Path(config['deployment_path'])
    root = deploy / '.logs' / 'auto-deploy-backups'
    backups = sorted((path for path in root.iterdir() if path.is_dir() and not path.is_symlink()
                      and re.fullmatch(r'auto-[0-9a-f]{12}-[0-9]+', path.name)),
                     key=lambda path: path.stat().st_mtime_ns, reverse=True)
    for backup in backups[keep:]:
        shutil.rmtree(backup)
        for component in ('backend', 'frontend'):
            paired = deploy / '.logs' / 'deploy-backups' / (backup.name + '-' + component)
            if paired.is_dir() and not paired.is_symlink():
                shutil.rmtree(paired)


def poll(config, state_path, retry=False, check=False):
    state = read_json(state_path) if state_path.exists() else {}
    sha = main_sha(config)
    if state.get('current_sha') == sha:
        return
    if state.get('failed_sha') == sha and not retry:
        log('This commit previously failed deployment; push a fix or request a local retry')
        return
    with tempfile.TemporaryDirectory(prefix='release-', dir=config['state_dir']) as temporary:
        work = Path(temporary)
        base = 'https://github.com/' + config['repository'] + '/releases/download/auto-' + sha + '/'
        if not download(base + 'manifest.json', work / 'manifest.json', 16384, missing_ok=True,
                        proxy=config.get('https_proxy')):
            log('Main ' + sha[:12] + ' has no checked release yet; keeping the current version')
            return
        manifest = read_json(work / 'manifest.json')
        validate_manifest(manifest, sha, config['runtime_generation'])
        log('Downloading checked release ' + sha[:12])
        for name, metadata in manifest['files'].items():
            download(base + name, work / name, metadata['size'], proxy=config.get('https_proxy'))
            verify_file(work / name, metadata)
        validate_frontend(work / 'frontend-build.tar.gz')
        if main_sha(config) != sha:
            log('Main changed while downloading; deferring to the next check')
            return
        if check:
            log('Checked release ' + sha[:12] + ' is ready; no program files were changed')
            return
        try:
            deploy_release(config, sha, work)
        except ActivationError:
            state.update(failed_sha=sha, failed_at=int(time.time()))
            write_state(state_path, state)
            raise
        state.update(current_sha=sha, deployed_at=int(time.time()))
        state.pop('failed_sha', None)
        state.pop('failed_at', None)
        write_state(state_path, state)
        log('Deployed and checked both program releases: ' + sha[:12])
        try:
            prune_backups(config)
        except OSError:
            log('Release is healthy, but old automatic backups could not be pruned')


def main():
    import fcntl  # Linux server only; pure validation tests also run on Windows.

    def interrupted(_signum, _frame):
        raise DeployError('Deployment interrupted')

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', default='/etc/vidlens-deploy.json')
    parser.add_argument('--retry', action='store_true', help='Retry a previously failed main commit once')
    parser.add_argument('--check', action='store_true', help='Download and validate without activating')
    args = parser.parse_args()
    config = load_config(Path(args.config))
    state_dir = Path(config['state_dir'])
    state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (state_dir / 'deploy.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            log('Another deployment is running')
            return
        poll(config, state_dir / 'state.json', retry=args.retry, check=args.check)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # No remote URLs, command output, environment values or application secrets.
        log('Automatic deployment failed: ' + (str(error) if isinstance(error, DeployError) else type(error).__name__))
        raise SystemExit(1)
