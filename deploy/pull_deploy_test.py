import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

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

    def download(self, url, path, limit, missing_ok=False):
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
