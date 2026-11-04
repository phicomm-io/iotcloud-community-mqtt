#!/usr/bin/env python3
"""Resolve release identity and bind published images to the pinned frontend."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import re
import subprocess

from release_version import parse_tag, parse_version

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'phicomm-io/iotcloud-community-mqtt'
COMMIT = re.compile(r'[0-9a-f]{40}\Z')
SHA256 = re.compile(r'[0-9a-f]{64}\Z')
DIGEST = re.compile(r'sha256:[0-9a-f]{64}\Z')
PLATFORMS = ['linux/amd64', 'linux/arm64']


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root, text=True).strip()


def read_core(root):
    text = (Path(root) / 'VERSION').read_text().strip()
    version = parse_version(text)
    if text != version.version or version.prerelease or version.core[0] < 1:
        raise ValueError('VERSION must contain a v1+ numeric core only')
    return version


def release_tag(root, tag):
    version = parse_tag(tag)
    if version.core[0] < 1 or version.core != read_core(root).core:
        raise ValueError('Release tag core must match VERSION; v0.9.x is frozen')
    return version


def pinned_lock(root, name, repository):
    data = (Path(root) / name).read_bytes()
    lock = json.loads(data)
    if (not isinstance(lock, dict) or lock.get('schema') != 1 or lock.get('repository') != repository
            or not isinstance(lock.get('commit'), str) or not COMMIT.fullmatch(lock['commit'])):
        raise ValueError('Invalid immutable pin: ' + name)
    if name == 'frontend.lock.json':
        if (lock.get('release') != 'frontend-' + lock['commit'] or lock.get('asset') != 'frontend.tar.gz'
                or not isinstance(lock.get('sha256'), str) or not SHA256.fullmatch(lock['sha256'])):
            raise ValueError('Invalid frontend release identity')
    elif not isinstance(lock.get('registry_sha256'), str) or not SHA256.fullmatch(lock['registry_sha256']):
        raise ValueError('Invalid protocol registry pin')
    return {**lock, 'lock_sha256': hashlib.sha256(data).hexdigest()}


def metadata(root, tag, commit):
    root = Path(root)
    version = release_tag(root, tag)
    if not isinstance(commit, str) or not COMMIT.fullmatch(commit) or git(root, 'rev-parse', 'HEAD') != commit:
        raise ValueError('Release commit must be the exact checkout HEAD')
    if git(root, 'rev-parse', tag + '^{commit}') != commit:
        raise ValueError('Release tag must point to the exact checkout HEAD')
    if git(root, 'status', '--porcelain'):
        raise ValueError('Release checkout must be clean')
    subprocess.run(['git', 'merge-base', '--is-ancestor', commit, 'origin/main'], cwd=root, check=True)
    return {
        'schema': 1, 'version': version.version,
        'version_core': '.'.join(map(str, version.core)), 'tag': version.tag,
        'channel': version.channel, 'prerelease': version.prerelease,
        'source': {'repository': REPOSITORY, 'commit': commit},
        'frontend': pinned_lock(root, 'frontend.lock.json', 'phicomm-io/iotcloud-community-frontend'),
        'protocols': pinned_lock(root, 'protocols.lock.json', 'phicomm-io/iotcloud-community-protocols'),
    }


def publication_settings(info):
    version = parse_tag(info['tag'])
    tags = ['type=ref,event=tag'] if version.prerelease else [
        'type=raw,value=latest', 'type=sha,format=long', 'type=ref,event=tag']
    build_json = json.dumps(info, sort_keys=True, separators=(',', ':'))
    # Preserve JSON quotes through the build action's CSV input parser.
    return {'version': version.version, 'tag': version.tag, 'channel': version.channel,
            'prerelease': str(version.prerelease).lower(), 'metadata_tags': '\n'.join(tags),
            'build_json': build_json,
            'build_json_base64': base64.b64encode(build_json.encode()).decode('ascii')}


def image_record(info, image_type, digest):
    if image_type not in ('backend', 'web') or not isinstance(digest, str) or not DIGEST.fullmatch(digest):
        raise ValueError('Expected backend/web and a complete published sha256 digest')
    return {'schema': 1, 'metadata': info, 'image_type': image_type,
            'image': 'ghcr.io/' + REPOSITORY + '-' + image_type,
            'tag': info['tag'], 'digest': digest, 'platforms': PLATFORMS}


def release_manifest(info, records):
    images = {}
    for record in records:
        if not isinstance(record, dict):
            raise ValueError('Published image record must be a JSON object')
        image_type = record.get('image_type')
        expected = image_record(info, image_type, record.get('digest', ''))
        if record != expected or image_type in images:
            raise ValueError('Published image metadata does not match this exact release')
        images[image_type] = {key: record[key] for key in ('image', 'tag', 'digest', 'platforms')}
    if set(images) != {'backend', 'web'}:
        raise ValueError('Both published backend and web digests are required')
    return {**info, 'images': images}


def write_json(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    for command in ('metadata', 'manifest'):
        child = sub.add_parser(command)
        child.add_argument('--root', type=Path, default=ROOT)
        child.add_argument('--tag', required=True)
        child.add_argument('--commit', required=True)
        child.add_argument('--output', type=Path, required=True)
        if command == 'metadata':
            child.add_argument('--github-output', type=Path)
        else:
            child.add_argument('--records', type=Path, required=True)
    child = sub.add_parser('image-record')
    child.add_argument('--metadata', type=Path, required=True)
    child.add_argument('--image-type', choices=('backend', 'web'), required=True)
    child.add_argument('--digest', required=True)
    child.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.command == 'image-record':
        result = image_record(json.loads(args.metadata.read_text()), args.image_type, args.digest)
    else:
        info = metadata(args.root, args.tag, args.commit)
        if args.command == 'manifest':
            result = release_manifest(info, [json.loads(path.read_text()) for path in sorted(args.records.glob('image-*.json'))])
        else:
            result = info
            if args.github_output:
                with args.github_output.open('a') as output:
                    for key, value in publication_settings(info).items():
                        if '\n' in value:
                            output.write(f'{key}<<PLATFORM_RELEASE_VALUE\n{value}\nPLATFORM_RELEASE_VALUE\n')
                        else:
                            output.write(f'{key}={value}\n')
    write_json(args.output, result)


if __name__ == '__main__':
    main()
