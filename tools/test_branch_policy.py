import contextlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from check_branch_policy import check


@contextlib.contextmanager
def working_directory(path):
    previous = os.getcwd()
    os.chdir(path)
    try:
        yield
    finally:
        os.chdir(previous)


class BranchPolicyTest(unittest.TestCase):
    def test_pr_routing(self):
        for base, head, allowed in (
            ('dev', 'feature/localization', True),
            ('dev', 'main', True),
            ('main', 'dev', True),
            ('main', 'hotfix/account', True),
            ('main', 'feature/localization', False),
            ('main', 'archive/0.9.x', False),
            ('archive/0.9.x', 'dev', False),
        ):
            event = {'pull_request': {'base': {'ref': base}, 'head': {'ref': head}}}
            with self.subTest(base=base, head=head):
                if allowed:
                    check('pull_request', event, '')
                else:
                    with self.assertRaises(ValueError):
                        check('pull_request', event, '')

    def test_stable_tags_and_real_main_ancestry(self):
        with tempfile.TemporaryDirectory() as directory, working_directory(directory):
            def git(*args):
                return subprocess.run(['git', *args], check=True, capture_output=True)

            git('init', '-b', 'main')
            git('config', 'user.name', 'Policy Test')
            git('config', 'user.email', 'policy@example.invalid')
            Path('fixture').write_text('stable\n')
            git('add', 'fixture')
            git('commit', '-m', 'stable')
            git('update-ref', 'refs/remotes/origin/main', 'HEAD')
            check('push', {}, 'refs/tags/v1.0.0')
            check('workflow_dispatch', {}, 'refs/tags/v10.2.3')
            git('checkout', '-b', 'dev')
            Path('fixture').write_text('unreleased\n')
            git('commit', '-am', 'unreleased')
            with self.assertRaises(subprocess.CalledProcessError):
                check('push', {}, 'refs/tags/v1.0.1')
            for ref in ('v0.9.99', 'v1.0.0-rc.1', 'v1.0.0+build', 'v1.00.0', 'v1.0', 'v01.0.0'):
                with self.subTest(ref=ref), self.assertRaises(ValueError):
                    check('push', {}, 'refs/tags/' + ref)

    def test_frozen_and_unknown_pushes(self):
        for branch in ('main', 'dev', 'feature/language', 'hotfix/session'):
            check('push', {}, 'refs/heads/' + branch)
        for branch in ('archive/0.9.x', 'master', 'temporary'):
            with self.subTest(branch=branch), self.assertRaises(ValueError):
                check('push', {}, 'refs/heads/' + branch)


if __name__ == '__main__':
    unittest.main()
