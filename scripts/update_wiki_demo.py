#!/usr/bin/env python3
"""Replace only the platform demo with the verified release product."""
import argparse
import json
from pathlib import Path
import re
import shutil


def update(site, bundle, tag, commit):
    if not re.fullmatch(r'v[A-Za-z0-9._+-]+', tag):
        raise ValueError('Expected a v-prefixed release tag')
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('Expected a full platform commit')
    source = bundle / 'demos/platform'
    if not (source / 'index.html').is_file():
        raise ValueError('Verified platform demo is missing')
    manifest = json.loads((bundle / 'manifest.json').read_text())
    destination = site / 'demos/platform'
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(source, destination)
    (destination / 'release.json').write_text(json.dumps({
        'repository': 'phicomm-io/phicomm-community-mqtt',
        'tag': tag, 'commit': commit,
        'frontend_commit': manifest['commit'],
    }, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', type=Path, required=True)
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--tag', required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    update(args.site, args.bundle, args.tag, args.commit)
