#!/usr/bin/env python3
"""Read-only migration audit. Outputs counts, never passwords or hashes."""
import argparse
import json
import sqlite3
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('--passwd', required=True, type=Path)
parser.add_argument('--acl', required=True, type=Path)
parser.add_argument('--runtime', required=True, type=Path)
args = parser.parse_args()
entries = []
for line in args.passwd.read_text().splitlines():
    line = line.strip()
    if not line or line.startswith('#'):
        continue
    if ':' in line:
        entries.append(line)
    else:
        assert entries, 'invalid password continuation'
        entries[-1] += line
original = dict(line.split(':', 1) for line in entries)
new = {c['username']: c for c in json.loads((args.runtime / 'broker/dynamic-security.json').read_text())['clients']}
for user, encoded in original.items():
    algorithm, iterations, salt, value = encoded.split('$')[1:]
    assert algorithm == '7' and user in new, 'missing account or unsupported hash'
    assert [str(new[user]['iterations']), new[user]['salt'], new[user]['password']] == [iterations, salt, value], 'password hash changed'
expected, owner = {}, None
for line in args.acl.read_text().splitlines():
    fields = line.strip().split()
    if not fields or fields[0].startswith('#'):
        continue
    if fields[0] == 'user':
        owner = fields[1]
        continue
    assert fields[:2] == ['topic', 'readwrite'], 'unsupported ACL'
    p = fields[2].split('/')
    if p[:2] == ['device', 'ztc1']:
        model, mac = 'tc1', p[2]
    elif p[0] == 'phicomm' and p[1] in ('m1', 'a1'):
        model, mac = p[1:3]
    elif p[:2] == ['homeassistant', '+']:
        model, mac = p[2].split('_', 1) if '_' in p[2] else ('tc1', p[2])
    else:
        raise AssertionError('unsupported ACL root')
    mac = mac.replace(':', '').replace('-', '').lower()
    old = expected.get(mac)
    assert not old or old[0] == owner, 'conflicting account ownership'
    if old and old[1] != 'tc1':
        assert model in ('tc1', old[1]), 'conflicting explicit model'
    else:
        expected[mac] = (owner, model)
db = sqlite3.connect(f'file:{args.runtime / "app/app.db"}?mode=ro', uri=True)
users = {r[0] for r in db.execute('SELECT username FROM users')}
assert set(original) <= users, 'missing database account'
actual = {}
for table in ('devices', 'scoped_devices'):
    for mac, user, model, version, status in db.execute(f'SELECT mac,username,model,version,status FROM {table}'):
        if status == 'hidden':
            continue
        assert mac not in actual, 'duplicate migrated device'
        actual[mac] = (user, model)
        assert version == ('v1' if model == 'tc1' else 'v2'), 'wrong protocol'
        assert table == ('devices' if model == 'tc1' else 'scoped_devices'), 'wrong namespace'
assert actual == expected, 'device identities, ownership or models changed'
assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok', 'database integrity failed'
assert not list(db.execute('PRAGMA foreign_key_check')), 'foreign key integrity failed'
print(json.dumps({'result': 'pass', 'original_accounts': len(original), 'identical_password_hashes': len(original), 'devices': len(actual), 'legacy_devices': sum(v[1] == 'tc1' for v in actual.values()), 'tmcp_devices': sum(v[1] != 'tc1' for v in actual.values()), 'ownership_and_models': 'identical after documented stub consolidation', 'sqlite_integrity': 'ok'}))
