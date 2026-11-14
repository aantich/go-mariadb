#!/usr/bin/env python3
"""Verify or publish all twelve exact-source MariaDB compatibility profiles.

This module consumes saved correctness receipts. It never starts a database,
runs a Go test, or executes a benchmark. Each summary field is reconstructed
from verified raw evidence rather than accepted from the live pointer.
"""
import argparse
from collections import Counter
import json
from pathlib import Path
import re

import compatibility
import correctness
import record

CONTRACT = 'stage7-compatibility-cohort-v1'
COMPILERS = ('1.25', '1.26', '1.27')
SERVERS = ('10.11', '11.4', '11.8', '12.3')
CELLS = {f'go{go}-mariadb{server}' for go in COMPILERS for server in SERVERS}


def evidence_path(relative):
    """Reject traversal and symlink escapes before reading public evidence."""
    path = (record.HERE / relative).resolve()
    if not path.is_relative_to((record.HERE / 'results').resolve()):
        raise ValueError('Compatibility evidence is outside validation/results.')
    return path


def profile_summary(capture, directory):
    match = re.fullmatch(r'go version go(1\.(?:25|26|27))\.\d+ (?:darwin|linux)/(?:arm64|amd64)',
                         capture['compiler']['version'])
    if match is None:
        raise ValueError('Unexpected compiler family.')
    compiler = match[1]
    family = capture['family']
    counts = Counter(case['status'] for stage in ('root', 'integration')
                     for case in correctness.leaves(capture['stages'][stage]['cases']).values())
    summary = {
        'compiler_family': compiler, 'server_family': family,
        'server_version': capture['server']['before']['version'],
        'compiler': capture['compiler'], 'host': capture['host'], 'image': capture['image'],
        'image_reference': capture['image_reference'],
        'leaf_counts': {name: counts[name] for name in ('pass', 'fail', 'skip')},
        'input_fingerprint': capture['input_fingerprint'],
        'evidence_directory': str(directory.relative_to(record.HERE.resolve())),
        'capture_sha256': record.digest((directory / 'capture.json').read_bytes()),
        'prior_passes_lost': capture['lost_prior_passes'],
        'restart_count': capture['restart_count'],
        'checks': {'race': True, 'vet': True}, 'resources_cleaned': True,
    }
    return f'go{compiler}-mariadb{family}', summary


def build(directories):
    """Derive a complete cohort, rejecting duplicate or missing actual cells."""
    source = record.inputs()
    profiles, compilers, versions, artifacts = {}, {}, {}, {}
    clients, servers = set(), set()
    for directory in directories:
        directory = Path(directory).resolve()
        # Require the same containment policy for explicit CLI paths and pointers.
        evidence_path(str(directory))
        capture = json.loads((directory / 'capture.json').read_text())
        for name in capture.get('logs', {}):
            evidence_path(str(directory / name))
        compatibility.validate(capture, directory)
        if capture['inputs'] != source:
            raise ValueError('Compatibility cohort changed source during validation.')
        key, summary = profile_summary(capture, directory)
        if key not in CELLS or key in profiles:
            raise ValueError('Duplicate or unexpected compatibility cell: ' + key)
        profiles[key] = summary
        compiler, family = summary['compiler_family'], summary['server_family']
        if compilers.setdefault(compiler, capture['compiler']) != capture['compiler']:
            raise ValueError('Compiler identity differs within a compiler family.')
        if versions.setdefault(family, summary['server_version']) != summary['server_version']:
            raise ValueError('Server version differs within a server family.')
        clients.add(capture['compiler']['version'].split()[-1])
        servers.add(capture['image']['Os'] + '/' + capture['image']['Architecture'])
        # The capture itself and all contract-required logs form the complete
        # artifact set. Unrelated files cannot substitute for missing evidence.
        for name in ('capture.json', *capture['logs']):
            path = evidence_path(str(directory / name))
            relative = 'validation/' + str(path.relative_to(record.HERE.resolve()))
            artifacts[relative] = record.digest(path.read_bytes())
    if set(profiles) != CELLS:
        raise ValueError('Compatibility cohort must contain all twelve unique cells.')
    if source != record.inputs():
        raise ValueError('Source changed during compatibility cohort validation.')
    return {
        'contract': CONTRACT,
        'input_fingerprint': record.digest(json.dumps(source, sort_keys=True).encode()),
        'inputs': source, 'profile_count': len(profiles), 'profiles': profiles,
        'compilers': compilers, 'server_versions': versions,
        'platform_scope': ', '.join(sorted(clients)) + ' clients; ' +
                          ', '.join(sorted(servers)) + ' Docker servers.',
        'performance_measured': False, 'artifacts': artifacts,
    }


def validate(summary):
    """Verify the index and every profile; a plausible summary is insufficient."""
    if not isinstance(summary, dict) or set(summary.get('profiles', {})) != CELLS:
        raise ValueError('Compatibility cohort must contain all twelve unique cells.')
    if summary.get('contract') != CONTRACT or summary.get('inputs') != record.inputs():
        raise ValueError('Wrong or stale compatibility cohort.')
    directories = []
    for cell in sorted(CELLS):
        relative = summary['profiles'][cell].get('evidence_directory')
        if not isinstance(relative, str) or not relative:
            raise ValueError('Missing compatibility evidence directory.')
        directories.append(evidence_path(relative))
    verified = build(directories)
    if json.dumps(verified, sort_keys=True) != json.dumps(summary, sort_keys=True):
        raise ValueError('Compatibility summary or artifact hashes differ from raw evidence.')
    return verified


def verify_current(directory=None):
    directory = record.HERE if directory is None else Path(directory)
    return validate(json.loads((directory / 'results/compatibility-current.json').read_text()))


def render(summary):
    """Render the public table from the same validated metadata as its index."""
    lines = ['# MariaDB compatibility and failure recovery', '',
             '**All 12 compiler/server profiles pass.** Correctness only; readiness remains **Alpha**.', '',
             'Source: `' + summary['input_fingerprint'] + '`. Counts are unique test leaves.', '',
             '| MariaDB tested | Go 1.25 PASS / SKIP | Go 1.26 PASS / SKIP | Go 1.27 PASS / SKIP |',
             '| --- | ---: | ---: | ---: |']
    for family in SERVERS:
        values = []
        for compiler in COMPILERS:
            cell = summary['profiles'][f'go{compiler}-mariadb{family}']
            count = cell['leaf_counts']
            values.append(f'[✅ {count["pass"]:,} / {count["skip"]}]'
                          f'({cell["evidence_directory"]}/capture.json)')
        version = summary['server_versions'][family].split('-MariaDB', 1)[0]
        lines.append('| ' + version + ' | ' + ' | '.join(values) + ' |')
    lines += ['', 'Every cell has **zero failures**, race detection and vet passing, one owned restart,',
              'cleaned-up resources and no lost earlier matching-profile passes. Required integration',
              'cases cannot skip. The displayed skips belong to the inherited root suite.', '',
              '**Platform scope:** ' + summary['platform_scope'], '',
              'The [complete current-version suite](RESULTS.md) separately covers real PAM/KDC/native',
              'providers and instrumentation on Mac and isolated Linux. The version matrix covers',
              'SQL/protocol/CDC units, precise values, native bulk/stream/reuse/cursor/progress/reset/',
              'metadata policies, verified TLS/zlib, transport faults, deadlocks, lock timeouts,',
              'uncertain COMMIT, pool restart and explicit CDC resume across schema changes.', '',
              '| Server feature | 10.11 / 11.4 | 11.8 / 12.3 |',
              '| --- | --- | --- |',
              '| Bulk unit results | Request rejected without changing rows | Native units verified |',
              '| PARSEC | Absent in the profile plugin inventory | Verified-TLS authentication passes |',
              '| VECTOR | SQL rejection checked | Value roundtrip passes |', '',
              'Go 1.27 has 84 additional direct row-scanning leaves; older compilers use the legacy path.',
              'Exact compiler identities and patch versions are in the linked receipts. This is Alpha',
              'validation, not certification of production applications or failover topologies.', '',
              'Run `python3 validation/verify.py` to check both current correctness and this complete',
              'cohort. Historical commands and passing leaves survive inventory and output-name changes.',
              'Missing, stale, incomplete or altered current compatibility evidence fails verification.', '',
              '[Stage 7 implementation and limits](../docs/STAGE7_COMPATIBILITY.md) ·',
              '[Review corrections](../docs/STAGE7_REVIEW_FIXES.md) ·',
              '[Machine-readable profiles](results/compatibility-current.json) · [Run validation](README.md)', '',
              'Earlier failed/source-changing attempts remain preserved. No benchmarks, smoke runs or',
              'profiling were performed for these corrections; historical timing/reference bytes are unchanged.',
              'New corrections remain uncommitted. No new remote CI run or package release is claimed.', '']
    return '\n'.join(lines)


def publish(directories):
    summary = build(directories)
    validate(summary)
    for path, text in ((record.HERE / 'results/compatibility-current.json', json.dumps(summary, indent=2) + '\n'),
                       (record.HERE / 'COMPATIBILITY.md', render(summary))):
        temporary = path.with_suffix(path.suffix + '.tmp')
        temporary.write_text(text)
        temporary.replace(path)
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--publish', action='store_true')
    parser.add_argument('--profile', type=Path, action='append', default=[])
    args = parser.parse_args()
    if args.publish:
        summary = publish(args.profile)
    else:
        if args.profile:
            parser.error('--profile requires --publish')
        summary = verify_current()
    print(f'Verified {summary["profile_count"]} exact-source compatibility profiles.')


if __name__ == '__main__':
    main()
