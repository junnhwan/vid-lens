import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
import urllib.error
from unittest.mock import patch
from unittest.mock import MagicMock

import pull_deploy as deploy


SHA = '0123456789abcdef0123456789abcdef01234567'
GENERATION = 'postgres-pgvector-v1'


class PullDeployTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.live = self.root / 'live'
        self.state = self.root / 'state'
        self.state.mkdir()
        (self.live / 'frontend' / 'dist').mkdir(parents=True)
        (self.live / 'server').write_text('old-backend')
        (self.live / 'frontend' / 'dist' / 'index.html').write_text('old-frontend')
        (self.live / '.runtime-generation').write_text(GENERATION + '\n')
        self.config = {
            'repository': 'example/vidlens', 'branch': 'main',
            'deployment_path': str(self.live), 'state_dir': str(self.state),
            'scripts_dir': str(self.root / 'scripts'), 'runtime_generation': GENERATION,
            'api_base': 'http://127.0.0.1:8080',
            'backend_health_url': 'http://127.0.0.1:8080/readyz',
            'frontend_health_url': 'http://127.0.0.1:3000/index.html',
        }
        self.frontend = self.archive([
            ('dist/index.html', b'new-frontend'), ('server.mjs', b'new-server'),
            ('package.json', b'{}'), ('package-lock.json', b'{}'),
        ])
        self.assets = {'server': b'new-backend', 'frontend-build.tar.gz': self.frontend}
        self.manifest = {
            'schema': 1, 'sha': SHA, 'os': 'linux', 'arch': 'amd64',
            'runtime_generation': GENERATION,
            'files': {name: {'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)}
                      for name, data in self.assets.items()},
        }

    def archive(self, members):
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
            for name, data in members:
                member = tarfile.TarInfo(name)
                if data is None:
                    member.type = tarfile.SYMTYPE
                    member.linkname = '/etc/passwd'
                    archive.addfile(member)
                else:
                    member.size = len(data)
                    archive.addfile(member, io.BytesIO(data))
        return buffer.getvalue()

    def download(self, url, path, limit, missing_ok=False, proxy=None):
        name = url.rsplit('/', 1)[-1]
        path.write_bytes(json.dumps(self.manifest).encode() if name == 'manifest.json' else self.assets[name])
        return True

    def test_manifest_rejects_wrong_commit_runtime_and_extra_assets(self):
        for key, value in [('sha', 'f' * 40), ('runtime_generation', 'other'), ('arch', 'arm64')]:
            with self.subTest(key=key), self.assertRaises(deploy.DeployError):
                deploy.validate_manifest(dict(self.manifest, **{key: value}), SHA, GENERATION)
        self.manifest['files']['config.yaml'] = self.manifest['files']['server']
        with self.assertRaises(deploy.DeployError):
            deploy.validate_manifest(self.manifest, SHA, GENERATION)

    def test_checksum_rejects_tampered_binary(self):
        binary = self.root / 'server'
        binary.write_bytes(b'tampered')
        with self.assertRaises(deploy.DeployError):
            deploy.verify_file(binary, self.manifest['files']['server'])

    def test_public_ref_remains_readable_when_git_transport_times_out(self):
        response = MagicMock()
        response.__enter__.return_value.read.return_value = json.dumps({
            'ref': 'refs/heads/main', 'object': {'type': 'commit', 'sha': SHA},
        }).encode()
        with patch.object(deploy, 'open_public', return_value=response, create=True), \
             patch.object(deploy.subprocess, 'run', side_effect=subprocess.TimeoutExpired('git', 45)):
            self.assertEqual(deploy.main_sha(self.config), SHA)

    def test_https_uses_configured_proxy_and_falls_back_without_exposing_values(self):
        response = MagicMock()
        with patch.object(deploy.urllib.request, 'build_opener') as build, \
             patch.object(deploy.urllib.request, 'ProxyHandler') as proxy_handler:
            build.return_value.open.side_effect = [urllib.error.URLError('unreachable'), response]
            self.assertIs(deploy.open_public('https://github.com/example', proxy='http://local-proxy.invalid'), response)
        self.assertEqual(proxy_handler.call_args_list[0].args, ({'https': 'http://local-proxy.invalid'},))
        self.assertEqual(proxy_handler.call_args_list[1].args, ({},))

    def test_rate_limited_proxy_retries_direct_connection(self):
        for status, remaining in ((403, '0'), (429, None)):
            with self.subTest(status=status):
                response = MagicMock()
                error = urllib.error.HTTPError('https://api.github.com/example', status, 'limited',
                                               {'X-RateLimit-Remaining': remaining}, None)
                opener = MagicMock()
                opener.open.side_effect = [error, response]
                with patch.object(deploy.urllib.request, 'build_opener', return_value=opener):
                    self.assertIs(deploy.open_public('https://api.github.com/example', proxy='http://local-proxy.invalid'), response)
                self.assertEqual(opener.open.call_count, 2)

    def test_other_http_errors_do_not_retry_direct(self):
        opener = MagicMock()
        opener.open.side_effect = urllib.error.HTTPError('https://github.com/example', 404, 'missing', {}, None)
        with patch.object(deploy.urllib.request, 'build_opener', return_value=opener), self.assertRaises(urllib.error.HTTPError):
            deploy.open_public('https://github.com/example', proxy='http://local-proxy.invalid')
        self.assertEqual(opener.open.call_count, 1)

    def test_public_ref_rejects_a_different_branch(self):
        response = MagicMock()
        response.__enter__.return_value.read.return_value = json.dumps({
            'ref': 'refs/heads/other', 'object': {'type': 'commit', 'sha': SHA},
        }).encode()
        with patch.object(deploy, 'open_public', return_value=response), self.assertRaises(deploy.DeployError):
            deploy.main_sha(self.config)

    def test_download_retries_alternate_route_after_a_midstream_timeout(self):
        interrupted, complete = MagicMock(), MagicMock()
        interrupted.__enter__.return_value.status = complete.__enter__.return_value.status = 200
        interrupted.__enter__.return_value.read.side_effect = [b'partial', TimeoutError('read stalled')]
        complete.__enter__.return_value.read.side_effect = [b'complete', b'']
        path = self.root / 'downloaded'
        with patch.object(deploy, 'open_public', side_effect=[interrupted, complete]) as request:
            self.assertTrue(deploy.download('https://github.com/asset', path, 32, proxy='http://local-proxy.invalid'))
        self.assertEqual(path.read_bytes(), b'complete')
        self.assertEqual(request.call_args_list[0].kwargs['route'], {'https': 'http://local-proxy.invalid'})
        self.assertEqual(request.call_args_list[1].kwargs['route'], {})

    def test_download_resumes_partial_bytes_on_alternate_route(self):
        interrupted, resumed = MagicMock(), MagicMock()
        interrupted.__enter__.return_value.status = 200
        interrupted.__enter__.return_value.read.side_effect = [b'part', TimeoutError('read stalled')]
        resumed.__enter__.return_value.status = 206
        resumed.__enter__.return_value.headers = {'Content-Range': 'bytes 4-7/8'}
        resumed.__enter__.return_value.read.side_effect = [b'ial!', b'']
        path = self.root / 'downloaded'
        with patch.object(deploy, 'open_public', side_effect=[interrupted, resumed]) as request:
            self.assertTrue(deploy.download('https://github.com/asset', path, 8, proxy='http://local-proxy.invalid'))
        self.assertEqual(path.read_bytes(), b'partial!')
        self.assertEqual(request.call_args_list[1].kwargs['range_start'], 4)
        deploy.verify_file(path, {'size': 8, 'sha256': hashlib.sha256(b'partial!').hexdigest()})

    def test_download_rejects_incorrect_resume_range(self):
        interrupted, resumed = MagicMock(), MagicMock()
        interrupted.__enter__.return_value.status = 200
        interrupted.__enter__.return_value.read.side_effect = [b'part', TimeoutError('read stalled')]
        resumed.__enter__.return_value.status = 206
        path = self.root / 'downloaded'
        for header in ('bytes 3-7/8', 'bytes 4-8/9', 'bytes 4-5/8', ''):
            with self.subTest(header=header):
                interrupted.__enter__.return_value.read.side_effect = [b'part', TimeoutError('read stalled')]
                resumed.__enter__.return_value.headers = {'Content-Range': header}
                with patch.object(deploy, 'open_public', side_effect=[interrupted, resumed]), self.assertRaises(deploy.DeployError):
                    deploy.download('https://github.com/asset', path, 8, proxy='http://local-proxy.invalid')
                self.assertEqual(path.read_bytes(), b'part')

    def test_download_rejects_incomplete_resume_body(self):
        interrupted, resumed = MagicMock(), MagicMock()
        interrupted.__enter__.return_value.status = 200
        interrupted.__enter__.return_value.read.side_effect = [b'part', TimeoutError('read stalled')]
        resumed.__enter__.return_value.status = 206
        resumed.__enter__.return_value.headers = {'Content-Range': 'bytes 4-7/8'}
        resumed.__enter__.return_value.read.side_effect = [b'ia', b'']
        with patch.object(deploy, 'open_public', side_effect=[interrupted, resumed]), self.assertRaises(deploy.NetworkError):
            deploy.download('https://github.com/asset', self.root / 'downloaded', 8, proxy='http://local-proxy.invalid')

    def test_public_resume_requests_only_the_missing_range(self):
        response = MagicMock()
        with patch.object(deploy.urllib.request, 'build_opener') as build:
            build.return_value.open.return_value = response
            deploy.open_public('https://github.com/asset', route={}, range_start=4)
        self.assertEqual(build.return_value.open.call_args.args[0].get_header('Range'), 'bytes=4-')

    def test_frontend_rejects_path_traversal_links_private_and_duplicate_files(self):
        for members in [ [('dist/../../escape', b'x')], [('dist/link', None)],
                         [('.env', b'secret')], [('dist/x', b'a'), ('dist/x', b'b')] ]:
            archive = self.root / 'frontend.tar.gz'
            archive.write_bytes(self.archive(members))
            with self.subTest(members=members), self.assertRaises(deploy.DeployError):
                deploy.validate_frontend(archive)

    def test_config_rejects_remote_health_and_unsafe_directories(self):
        path = self.root / 'config.json'
        for key, value in [('backend_health_url', 'https://example.com/readyz'),
                           ('deployment_path', '/'), ('branch', 'other'),
                           ('repository', 'example/vidlens;command')]:
            path.write_text(json.dumps(dict(self.config, **{key: value})))
            with self.subTest(key=key), self.assertRaises(deploy.DeployError):
                deploy.load_config(path)

    def test_unpublished_commit_keeps_running_release(self):
        with patch.object(deploy, 'main_sha', return_value=SHA), \
             patch.object(deploy, 'download', return_value=False), \
             patch.object(deploy, 'deploy_release') as apply:
            deploy.poll(self.config, self.state / 'state.json')
        apply.assert_not_called()
        self.assertEqual((self.live / 'server').read_text(), 'old-backend')

    def test_new_push_during_download_prevents_stale_deployment(self):
        with patch.object(deploy, 'main_sha', side_effect=[SHA, 'f' * 40]), \
             patch.object(deploy, 'download', side_effect=self.download), \
             patch.object(deploy, 'deploy_release') as apply:
            deploy.poll(self.config, self.state / 'state.json')
        apply.assert_not_called()

    def test_success_records_commit_and_skips_repeated_deploy(self):
        state_path = self.state / 'state.json'
        with patch.object(deploy, 'main_sha', return_value=SHA), \
             patch.object(deploy, 'download', side_effect=self.download), \
             patch.object(deploy, 'deploy_release') as apply:
            deploy.poll(self.config, state_path)
            deploy.poll(self.config, state_path)
        self.assertEqual(apply.call_count, 1)
        self.assertEqual(deploy.read_json(state_path)['current_sha'], SHA)

    def test_failed_commit_requires_new_push_or_explicit_retry(self):
        state_path = self.state / 'state.json'
        with patch.object(deploy, 'main_sha', return_value=SHA), \
             patch.object(deploy, 'download', side_effect=self.download), \
             patch.object(deploy, 'deploy_release', side_effect=deploy.ActivationError('failed')) as apply:
            with self.assertRaises(deploy.DeployError):
                deploy.poll(self.config, state_path)
            deploy.poll(self.config, state_path)
            self.assertEqual(apply.call_count, 1)
            with self.assertRaises(deploy.DeployError):
                deploy.poll(self.config, state_path, retry=True)
            self.assertEqual(apply.call_count, 2)
        self.assertEqual(deploy.read_json(state_path)['failed_sha'], SHA)

    def test_unhealthy_existing_service_is_retried_without_switching(self):
        state_path = self.state / 'state.json'
        with patch.object(deploy, 'main_sha', return_value=SHA), \
             patch.object(deploy, 'download', side_effect=self.download), \
             patch.object(deploy, 'deploy_release', side_effect=deploy.DeployError('not ready')) as apply:
            for _ in range(2):
                with self.assertRaises(deploy.DeployError):
                    deploy.poll(self.config, state_path)
        self.assertEqual(apply.call_count, 2)
        self.assertFalse(state_path.exists())

    def test_pruning_retains_five_auto_backups_and_all_manual_backups(self):
        root = self.live / '.logs' / 'auto-deploy-backups'
        root.mkdir(parents=True)
        for number in range(8):
            (root / ('auto-' + SHA[:12] + '-' + str(number))).mkdir()
        manual = root / 'manual-release'
        manual.mkdir()
        deploy.prune_backups(self.config)
        self.assertEqual(len(list(root.iterdir())), 6)
        self.assertTrue(manual.exists())

    def test_frontend_failure_restores_both_program_versions(self):
        work = self.state / 'work'
        work.mkdir()
        for name, data in self.assets.items():
            (work / name).write_bytes(data)

        def command(arguments, environment=None):
            if arguments[0] == 'bash' and arguments[1].endswith('server-deploy.sh'):
                (self.live / 'server').write_text('new-backend')
            if arguments[0] == 'bash' and arguments[1].endswith('frontend-deploy.sh'):
                (self.live / 'frontend' / 'dist' / 'index.html').write_text('bad-frontend')
                raise deploy.DeployError('frontend failed')

        with patch.object(deploy, 'health'), patch.object(deploy, 'command', side_effect=command):
            with self.assertRaises(deploy.DeployError):
                deploy.deploy_release(self.config, SHA, work)
        self.assertEqual((self.live / 'server').read_text(), 'old-backend')
        self.assertEqual((self.live / 'frontend' / 'dist' / 'index.html').read_text(), 'old-frontend')

    def test_validation_only_does_not_activate_or_record_release(self):
        state_path = self.state / 'state.json'
        with patch.object(deploy, 'main_sha', return_value=SHA), \
             patch.object(deploy, 'download', side_effect=self.download), \
             patch.object(deploy, 'deploy_release') as apply:
            deploy.poll(self.config, state_path, check=True)
        apply.assert_not_called()
        self.assertFalse(state_path.exists())


if __name__ == '__main__':
    unittest.main()
