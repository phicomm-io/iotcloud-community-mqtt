import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest

from platform_release import (ROOT, image_record, metadata, publication_settings,
                              read_core, release_manifest, release_tag)
from release_version import parse_tag, parse_version, version_key


class ReleaseGrammarTest(unittest.TestCase):
    def test_candidate_and_stable_order(self):
        values = ['1.0.0-rc', '1.0.0-rc.0', '1.0.0-rc.1', '1.0.0-rc.12', '1.0.0', '1.0.1-rc']
        self.assertEqual(values, sorted(reversed(values), key=version_key))
        for value in values:
            with self.subTest(value=value):
                version = parse_tag('v' + value)
                self.assertEqual(version.version, value)
                self.assertEqual(version, parse_version(value))
                self.assertEqual(version.channel, 'candidate' if '-rc' in value else 'stable')

    def test_strict_grammar(self):
        for value in ('1.0', '01.0.0', '1.00.0', '1.0.01', '1.0.0-rc.01', '1.0.0-rc.-1',
                      '1.0.0-rc.', '1.0.0-rc.1.2', '1.0.0-beta', '1.0.0+build',
                      '1.0.0\n', ' 1.0.0', '１.0.0', '', None, 1):
            with self.subTest(value=value), self.assertRaises(ValueError):
                parse_version(value)
        with self.assertRaises(ValueError):
            parse_tag('1.0.0-rc')


class ReleaseFixture(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.git('init', '-b', 'main')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'release@example.invalid')
        (self.root / 'VERSION').write_text('1.0.0\n')
        (self.root / '.gitignore').write_text('build/\n')
        self.frontend = {'schema': 1, 'repository': 'phicomm-io/iotcloud-community-frontend',
                         'commit': 'a' * 40, 'release': 'frontend-' + 'a' * 40,
                         'asset': 'frontend.tar.gz', 'sha256': 'b' * 64}
        self.protocols = {'schema': 1, 'repository': 'phicomm-io/iotcloud-community-protocols',
                          'commit': 'c' * 40, 'registry_sha256': 'd' * 64,
                          'contracts': ['tmcp-v2', 'platform-session-v1', 'daikin-hub-v1']}
        self.write_lock('frontend.lock.json', self.frontend)
        self.write_lock('protocols.lock.json', self.protocols)
        self.git('add', '.')
        self.git('commit', '-m', 'release fixture')
        self.commit = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', self.commit)
        self.git('tag', 'v1.0.0-rc')
        self.git('tag', 'v1.0.0')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True, stderr=subprocess.DEVNULL).strip()

    def write_lock(self, name, value):
        (self.root / name).write_text(json.dumps(value, indent=2) + '\n')

    def info(self, tag='v1.0.0-rc'):
        return metadata(self.root, tag, self.commit)

    def test_exact_metadata_and_pins(self):
        info = self.info()
        self.assertEqual(info['version'], '1.0.0-rc')
        self.assertEqual(info['version_core'], '1.0.0')
        self.assertEqual(info['source']['commit'], self.commit)
        self.assertTrue(info['prerelease'])
        for key, name in (('frontend', 'frontend.lock.json'), ('protocols', 'protocols.lock.json')):
            self.assertEqual(info[key]['lock_sha256'], hashlib.sha256((self.root / name).read_bytes()).hexdigest())
        (self.root / 'build').mkdir()
        (self.root / 'build' / 'version.json').write_text('ignored build output')
        self.assertEqual(info, self.info())

    def test_wrong_core_old_tag_and_noncore_version_are_rejected(self):
        for tag in ('v0.9.27', 'v1.0.1-rc'):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release_tag(self.root, tag)
        for version in ('1.0.0-rc', 'v1.0.0', '0.9.27'):
            (self.root / 'VERSION').write_text(version + '\n')
            with self.subTest(version=version), self.assertRaises(ValueError):
                read_core(self.root)

    def test_dirty_wrong_commit_and_wrong_tag_are_rejected(self):
        with self.assertRaises(ValueError):
            metadata(self.root, 'v1.0.0-rc', 'f' * 40)
        (self.root / 'untracked').write_text('not part of the release')
        with self.assertRaises(ValueError):
            self.info()
        self.git('add', 'untracked')
        self.git('commit', '-m', 'different source')
        new_commit = self.git('rev-parse', 'HEAD')
        with self.assertRaises(ValueError):
            metadata(self.root, 'v1.0.0-rc', new_commit)

    def test_exact_tag_outside_main_is_rejected(self):
        (self.root / 'feature').write_text('unmerged')
        self.git('add', 'feature')
        self.git('commit', '-m', 'unmerged source')
        self.git('tag', 'v1.0.0-rc.1')
        with self.assertRaises(subprocess.CalledProcessError):
            metadata(self.root, 'v1.0.0-rc.1', self.git('rev-parse', 'HEAD'))

    def test_invalid_immutable_locks_are_rejected(self):
        for invalid in (None, [], {**self.frontend, 'commit': None},
                        {**self.frontend, 'sha256': 'b' * 63},
                        {**self.frontend, 'release': 'frontend-main'}):
            self.write_lock('frontend.lock.json', invalid)
            self.git('add', 'frontend.lock.json')
            self.git('commit', '--allow-empty', '-m', 'invalid lock')
            current = self.git('rev-parse', 'HEAD')
            self.git('tag', '-f', 'v1.0.0-rc')
            self.git('update-ref', 'refs/remotes/origin/main', current)
            with self.subTest(lock=invalid), self.assertRaises(ValueError):
                metadata(self.root, 'v1.0.0-rc', current)

    def test_candidate_cannot_select_stable_image_aliases(self):
        for tag in ('v1.0.0-rc', 'v1.0.0-rc.0', 'v1.0.0-rc.12'):
            info = {**self.info(), 'tag': tag, 'version': tag[1:]}
            settings = publication_settings(info)
            self.assertEqual(settings['metadata_tags'].splitlines(), ['type=ref,event=tag'])
            self.assertEqual(settings['prerelease'], 'true')
            self.assertEqual(json.loads(settings['build_json']), info)
        stable = publication_settings(self.info('v1.0.0'))
        self.assertEqual(stable['metadata_tags'].splitlines(),
                         ['type=raw,value=latest', 'type=sha,format=long', 'type=ref,event=tag'])
        self.assertEqual(stable['prerelease'], 'false')

    def test_build_metadata_transport_preserves_nested_arrays_and_special_characters(self):
        info = self.info()
        info['protocols']['contracts'] = ['tmcp-v2', 'platform-session-v1', 'daikin-hub-v1']
        info['transport_example'] = {'text': '中文, "quotes" \\ slash\nline', 'values': [False, None, 12]}
        settings = publication_settings(info)
        encoded = settings['build_json_base64']
        self.assertRegex(encoded, r'\A[A-Za-z0-9+/]+={0,2}\Z')
        decoded = base64.b64decode(encoded, validate=True).decode()
        self.assertEqual(decoded, settings['build_json'])
        self.assertEqual(json.loads(decoded), info)

    def test_manifest_binds_both_images_source_frontend_and_digest(self):
        info = self.info()
        records = [image_record(info, image, 'sha256:' + digest * 64)
                   for image, digest in (('backend', '1'), ('web', '2'))]
        manifest = release_manifest(info, records)
        self.assertEqual(manifest['frontend'], info['frontend'])
        self.assertEqual(manifest['images']['backend']['digest'], 'sha256:' + '1' * 64)
        self.assertEqual(manifest['images']['web']['tag'], 'v1.0.0-rc')
        for invalid in ([], records[:1], [records[0], records[0]], [None, records[1]]):
            with self.subTest(records=invalid), self.assertRaises(ValueError):
                release_manifest(info, invalid)
        for field, value in (('digest', None), ('digest', 'sha256:' + '1' * 63),
                             ('tag', 'v1.0.0'), ('platforms', ['linux/amd64']),
                             ('image', 'ghcr.io/unrelated/web')):
            wrong = copy.deepcopy(records)
            wrong[1][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                release_manifest(info, wrong)
        wrong = copy.deepcopy(records)
        wrong[1]['metadata']['frontend']['commit'] = 'e' * 40
        with self.assertRaises(ValueError):
            release_manifest(info, wrong)

    def test_cli_outputs_exact_build_identity(self):
        output = self.root / 'build' / 'version.json'
        output.parent.mkdir()
        github_output = self.root / 'build' / 'github-output'
        subprocess.run(['python3', str(ROOT / 'tools' / 'platform_release.py'), 'metadata',
                        '--root', str(self.root), '--tag', 'v1.0.0-rc', '--commit', self.commit,
                        '--output', str(output), '--github-output', str(github_output)], check=True)
        self.assertEqual(json.loads(output.read_text()), self.info())
        settings = github_output.read_text()
        self.assertIn('prerelease=true\n', settings)
        self.assertIn('metadata_tags=type=ref,event=tag\n', settings)
        self.assertIn('build_json_base64=' + publication_settings(self.info())['build_json_base64'] + '\n', settings)
        self.assertNotIn('latest', settings)


class WorkflowPublicationTest(unittest.TestCase):
    @staticmethod
    def script(name):
        workflow = (ROOT / '.github/workflows/platform.yml').read_text()
        step = workflow.split('      - name: ' + name + '\n', 1)[1].split('\n      - ', 1)[0]
        return textwrap.dedent(step.split('        run: |\n', 1)[1])

    def run_step(self, script, prerelease=True, exists=False, published=None, api_status='404', curated_notes=True):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'build').mkdir()
            (root / 'frontend.lock.json').write_text('exact frontend pin\n')
            (root / 'build/release-manifest.json').write_text('exact source and image manifest\n')
            if curated_notes:
                notes = root / 'docs/releases/v1.0.0-rc.md'
                notes.parent.mkdir(parents=True)
                notes.write_text('Curated features for release candidates; no generated implementation history.\n')
            published_dir = root / 'published'
            published_dir.mkdir()
            for name, content in (published or {}).items():
                (published_dir / name).write_text(content)
            gh = root / 'gh'
            gh.write_text(textwrap.dedent('''\
                #!/usr/bin/env python3
                import json, os, pathlib, shutil, sys
                args = sys.argv[1:]
                with open(os.environ['GH_LOG'], 'a') as log:
                    log.write(json.dumps(args) + '\\n')
                if args[0] == 'api':
                    status = os.environ['GH_API_STATUS']
                    if status != 'network-error':
                        print('HTTP/2.0 ' + status + ' fixture status')
                    sys.exit(0 if status == '200' else 1)
                if args[:2] == ['release', 'view']:
                    if '--json' in args:
                        print('\\n'.join(p.name for p in pathlib.Path(os.environ['GH_PUBLISHED']).iterdir()))
                        sys.exit(0)
                    sys.exit(int(os.environ['GH_VIEW_STATUS']))
                if args[:2] == ['release', 'download']:
                    name = args[args.index('--pattern') + 1]
                    destination = pathlib.Path(args[args.index('--dir') + 1]) / name
                    shutil.copyfile(pathlib.Path(os.environ['GH_PUBLISHED']) / name, destination)
                '''))
            gh.chmod(0o755)
            log = root / 'calls.jsonl'
            tag = 'v1.0.0-rc' if prerelease else 'v1.0.0'
            env = {**os.environ, 'PATH': str(root) + os.pathsep + os.environ['PATH'],
                   'RELEASE_TAG': tag, 'PRERELEASE': str(prerelease).lower(),
                   'GITHUB_REPOSITORY': 'phicomm-io/iotcloud-community-mqtt',
                   'GH_LOG': str(log), 'GH_VIEW_STATUS': '0' if exists else '1',
                   'GH_PUBLISHED': str(published_dir), 'GH_API_STATUS': api_status}
            result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', script], cwd=root,
                                    env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            return result, calls

    def test_published_images_use_checked_metadata_without_implicit_latest(self):
        workflow = (ROOT / '.github/workflows/platform.yml').read_text()
        self.assertIn('flavor: latest=false', workflow)
        self.assertIn('tags: ${{ steps.version.outputs.metadata_tags }}', workflow)
        self.assertIn('BUILD_VERSION_JSON_BASE64=${{ steps.version.outputs.build_json_base64 }}', workflow)
        self.assertNotIn('BUILD_VERSION_JSON=${{', workflow)
        self.assertIn("python3 -m unittest discover -s tools -p 'test_*.py' -v", workflow)
        self.assertNotIn('docker compose', workflow)

    def test_candidate_existing_release_or_failed_query_cannot_rebuild_images(self):
        script = self.script('Preserve published candidate images')
        workflow = (ROOT / '.github/workflows/platform.yml').read_text()
        self.assertLess(workflow.index('name: Preserve published candidate images'), workflow.index('name: Build and publish'))
        for status, allowed in (('404', True), ('200', False), ('500', False), ('network-error', False)):
            with self.subTest(status=status):
                result, calls = self.run_step(script, api_status=status)
                self.assertEqual(result.returncode == 0, allowed, result.stderr)
                self.assertEqual(calls, [['api', '--include', '--silent',
                                         'repos/phicomm-io/iotcloud-community-mqtt/releases/tags/v1.0.0-rc']])

    def test_actual_release_step_new_candidate_does_not_promote_or_clobber(self):
        result, calls = self.run_step(self.script('Publish GitHub release'))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(['release', 'create', 'v1.0.0-rc', '--verify-tag', '--notes-file', 'docs/releases/v1.0.0-rc.md',
                       '--prerelease', '--latest=false'], calls)
        self.assertEqual(calls[-1], ['release', 'upload', 'v1.0.0-rc', 'frontend.lock.json', 'build/release-manifest.json'])
        self.assertTrue(all('--clobber' not in call for call in calls))
        self.assertTrue(all('--generate-notes' not in call for call in calls))

    def test_candidate_missing_curated_notes_fails_before_any_api_or_write(self):
        for step in ('Preserve published candidate images', 'Publish GitHub release'):
            with self.subTest(step=step):
                result, calls = self.run_step(self.script(step), curated_notes=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Curated candidate release notes are missing', result.stdout)
                self.assertEqual(calls, [])

    def test_actual_candidate_assets_identical_are_reused_missing_are_added(self):
        assets = {'frontend.lock.json': 'exact frontend pin\n',
                  'release-manifest.json': 'exact source and image manifest\n'}
        for missing in (None, 'frontend.lock.json', 'release-manifest.json'):
            published = {key: value for key, value in assets.items() if key != missing}
            with self.subTest(missing=missing):
                result, calls = self.run_step(self.script('Publish GitHub release'), exists=True, published=published)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(['release', 'edit', 'v1.0.0-rc', '--notes-file', 'docs/releases/v1.0.0-rc.md',
                               '--prerelease', '--latest=false'], calls)
                uploads = [call for call in calls if call[:2] == ['release', 'upload']]
                expected = [] if missing is None else [['release', 'upload', 'v1.0.0-rc',
                                                        missing if missing == 'frontend.lock.json' else 'build/' + missing]]
                self.assertEqual(uploads, expected)
                self.assertTrue(all('--clobber' not in call for call in calls))
                self.assertTrue(all('--generate-notes' not in call for call in calls))

    def test_actual_candidate_asset_conflicts_fail_before_any_write(self):
        for asset in ('frontend.lock.json', 'release-manifest.json'):
            with self.subTest(asset=asset):
                result, calls = self.run_step(self.script('Publish GitHub release'), exists=True,
                                              published={asset: 'different immutable bytes\n'})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Published candidate asset differs', result.stdout)
                self.assertFalse(any(call[:2] in (['release', 'upload'], ['release', 'edit'], ['release', 'create'])
                                     for call in calls))

    def test_actual_stable_release_step_retains_original_behavior(self):
        for exists in (False, True):
            with self.subTest(exists=exists):
                result, calls = self.run_step(self.script('Publish GitHub release'), prerelease=False, exists=exists)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(calls[0], ['release', 'view', 'v1.0.0'])
                self.assertEqual(calls[-1], ['release', 'upload', 'v1.0.0', 'frontend.lock.json',
                                            'build/release-manifest.json', '--clobber'])
                expected = [] if exists else [['release', 'create', 'v1.0.0', '--verify-tag', '--generate-notes']]
                self.assertEqual(calls[1:-1], expected)


if __name__ == '__main__':
    unittest.main()
