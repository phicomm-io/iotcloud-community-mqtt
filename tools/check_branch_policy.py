#!/usr/bin/env python3
"""Check PR routing and require release tags to belong to main."""
import json
import os
from pathlib import Path
import subprocess

from platform_release import release_tag


def check(event_name, event, ref):
    if event_name == 'pull_request':
        pull = event['pull_request']
        base, head = pull['base']['ref'], pull['head']['ref']
        allowed = ((base == 'dev' and (head.startswith(('feature/', 'hotfix/')) or head == 'main')) or
                   (base == 'main' and (head == 'dev' or head.startswith('hotfix/'))))
        if not allowed:
            raise ValueError('Use feature/* -> dev -> main; hotfix/* -> main, then sync main -> dev.')
    elif ref.startswith('refs/tags/v'):
        release_tag(Path.cwd(), ref.removeprefix('refs/tags/'))
        subprocess.run(['git', 'merge-base', '--is-ancestor', 'HEAD', 'origin/main'], check=True)
    elif event_name in ('push', 'workflow_dispatch'):
        branch = ref.removeprefix('refs/heads/')
        if branch not in ('main', 'dev') and not branch.startswith(('feature/', 'hotfix/')):
            raise ValueError('Build main, dev, feature/* or hotfix/*; archive/0.9.x is frozen.')


if __name__ == '__main__':
    check(os.environ.get('GITHUB_EVENT_NAME', ''),
          json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text()),
          os.environ.get('GITHUB_REF', ''))
