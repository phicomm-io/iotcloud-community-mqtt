#!/usr/bin/env python3
"""Fetch a frontend release pinned by full commit and archive SHA-256.

Canonical source: iotcloud-community-frontend/tools/fetch_release.py.
The firmware and backend carry this bootstrap consumer, not UI source.
"""
import argparse
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile
import tempfile
from urllib.request import Request, urlopen


def read_lock(path):
    lock = json.loads(Path(path).read_text())
    if lock.get('schema') != 1 or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', lock.get('repository', '')):
        raise ValueError('Invalid frontend repository or lock schema')
    if not re.fullmatch(r'[0-9a-f]{40}', lock.get('commit', '')):
        raise ValueError('Frontend lock requires a full 40-character commit')
    if lock.get('release') != 'frontend-' + lock['commit'] or lock.get('asset') != 'frontend.tar.gz':
        raise ValueError('Release tag and asset must identify the pinned commit')
    if not re.fullmatch(r'[0-9a-f]{64}', lock.get('sha256', '')):
        raise ValueError('Frontend lock requires archive SHA-256')
    return lock


def safe_path(name):
    path = PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or '\\' in name or str(path) != name or not path.parts:
        raise ValueError(f'Unsafe frontend archive path: {name}')
    return path


def unpack(data, lock, output):
    if hashlib.sha256(data).hexdigest() != lock['sha256']:
        raise ValueError('Frontend archive SHA-256 mismatch')
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as tar:
        members = tar.getmembers()
        names = set()
        for member in members:
            safe_path(member.name)
            if not member.isfile() or member.name in names:
                raise ValueError('Frontend archive contains a link, directory or duplicate entry')
            names.add(member.name)
        manifest = json.load(tar.extractfile('manifest.json'))
        if manifest.get('schema') != 1 or manifest.get('commit') != lock['commit'] or manifest.get('repository') != lock['repository']:
            raise ValueError('Frontend manifest commit/repository mismatch')
        if names != set(manifest['files']) | {'manifest.json'}:
            raise ValueError('Frontend archive file list mismatch')
        required = {'platform/index.html', 'demos/platform/index.html'}
        for model in ('tc1', 'm1', 'a1'):
            required.update({f'devices/{model}/index.html', f'devices/{model}/index.html.gz', f'demos/{model}/index.html'})
        if 'daikin' in manifest.get('features', []):
            required.update({'devices/daikin/index.html', 'devices/daikin/index.html.gz', 'demos/daikin/index.html'})
        if 'local-console' in manifest.get('features', []):
            required.add('local/index.html')
        if 'bootstrap-ui' in manifest.get('features', []):
            for model in ('tc1', 'm1', 'a1'):
                required.update({f'devices/{model}/setup.html', f'devices/{model}/setup.html.gz'})
        if not required <= names:
            raise ValueError('Frontend release is missing required products')
        for member in members:
            content = tar.extractfile(member).read()
            if member.name != 'manifest.json' and hashlib.sha256(content).hexdigest() != manifest['files'][member.name]:
                raise ValueError(f'Frontend file checksum mismatch: {member.name}')
            dest = output / member.name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(content)


def fetch(lock_path, output=None, archive=None):
    lock_path = Path(lock_path).resolve()
    lock = read_lock(lock_path)
    output = Path(output).resolve() if output else lock_path.parent / 'build/frontend' / lock['commit']
    output.parent.mkdir(parents=True, exist_ok=True)
    with (output.parent / (output.name + '.lock')).open('a') as guard:
        fcntl.flock(guard, fcntl.LOCK_EX)
        cache = output.parent / (lock['sha256'] + '.tar.gz')
        archive = archive or os.environ.get('PHICOMM_FRONTEND_ARCHIVE')
        if archive:
            data = Path(archive).read_bytes()
        elif cache.is_file():
            data = cache.read_bytes()
        else:
            url = f"https://github.com/{lock['repository']}/releases/download/{lock['release']}/{lock['asset']}"
            with urlopen(Request(url, headers={'User-Agent': 'phicomm-frontend-build'}), timeout=120) as response:
                data = response.read()
        with tempfile.TemporaryDirectory(prefix='.frontend-', dir=output.parent) as temp:
            staged = Path(temp) / 'verified'
            unpack(data, lock, staged)
            # A bad artifact never replaces the previous verified directory.
            if output.exists():
                shutil.rmtree(output)
            staged.rename(output)
        cache.write_bytes(data)
    return output


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lock', type=Path, default=Path(__file__).resolve().parents[1] / 'frontend.lock.json')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--archive', type=Path, help='Offline archive; must match the committed lock')
    args = parser.parse_args()
    print(fetch(args.lock, args.output, args.archive))
