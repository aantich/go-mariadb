#!/usr/bin/env python3
"""Publish verified application evidence: eight version cells plus native Linux."""
import argparse
from collections import Counter
import json
from pathlib import Path
import re

import applications
import applications_local
from compatibility_cohort import evidence_path
import correctness
import record

CONTRACT='stage8-applications-cohort-v1'
COMPILERS=('1.25','1.27')
SERVERS=('10.11','11.4','11.8','12.3')
CELLS={f'go{go}-mariadb{server}' for go in COMPILERS for server in SERVERS}
NATIVE='go1.27-native-linux'


def build(directories):
    """Reconstruct every displayed value from current, hash-bound raw receipts."""
    source=record.inputs();profiles={};artifacts={};compilers={};versions={};clients=set()
    for directory in directories:
        directory=evidence_path(str(Path(directory).resolve()))
        capture=json.loads((directory/'capture.json').read_text())
        native=bool(capture.get('native_contract'))
        (applications_local.validate if native else applications.validate)(capture,directory)
        match=re.fullmatch(r'go version go(1\.(?:25|27))\.\d+ (darwin|linux)/(arm64|amd64)',capture['compiler']['version'])
        if match is None:raise ValueError('Unexpected application compiler.')
        compiler=match[1];family=capture['server']['before']['version'].split('-')[0].rsplit('.',1)[0]
        key=NATIVE if native and compiler=='1.27' else f'go{compiler}-mariadb{family}'
        if key not in CELLS|{NATIVE} or key in profiles or native and key!=NATIVE:
            raise ValueError('Duplicate or unexpected application cell: '+key)
        counts=Counter(case['status'] for stage in applications.REQUIRED
            for case in correctness.leaves(capture['stages'][stage]['cases']).values())
        profiles[key]={'compiler':capture['compiler'],'host':capture['host'],
            'server_version':capture['server']['before']['version'],
            'transport':'private-unix-socket' if native else 'loopback Docker TCP',
            'leaf_counts':{name:counts[name] for name in ('pass','fail','skip')},
            'evidence_directory':str(directory.relative_to(record.HERE.resolve())),
            'capture_sha256':record.digest((directory/'capture.json').read_bytes()),
            'restart_count':capture['restart_count'],'resources_cleaned':capture['resources_cleaned'],
            'checks':{'race':True,'vet':True,'independent_consumer':True,'locked_modules':True}}
        if counts['skip'] or counts['fail']:raise ValueError('Applications require every case to pass.')
        if not native:
            clients.add(match[2]+'/'+match[3])
            if compilers.setdefault(compiler,capture['compiler'])!=capture['compiler']:
                raise ValueError('Application compiler identity changed within a family.')
            if versions.setdefault(family,capture['server']['before']['version'])!=capture['server']['before']['version']:
                raise ValueError('Application server patch changed within a family.')
        for name in ('capture.json',*capture['logs']):
            path=evidence_path(str(directory/name))
            artifacts['validation/'+str(path.relative_to(record.HERE.resolve()))]=record.digest(path.read_bytes())
    if set(profiles)!=CELLS|{NATIVE}:raise ValueError('Require all eight application cells and native Linux.')
    if source!=record.inputs():raise ValueError('Application cohort source changed during validation.')
    return {'contract':CONTRACT,'inputs':source,
        'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
        'profile_count':len(profiles),'profiles':profiles,'compilers':compilers,
        'server_versions':versions,
        'platform_scope':', '.join(sorted(clients))+' matrix clients; supplementary native Linux private Unix-socket client.',
        'performance_measured':False,'artifacts':artifacts}


def validate(summary):
    if summary.get('contract')!=CONTRACT or summary.get('inputs')!=record.inputs():
        raise ValueError('Wrong or stale application cohort.')
    if set(summary.get('profiles',{}))!=CELLS|{NATIVE}:raise ValueError('Incomplete application cohort.')
    directories=[evidence_path(summary['profiles'][key]['evidence_directory']) for key in sorted(CELLS|{NATIVE})]
    verified=build(directories)
    if summary!=verified:raise ValueError('Application summary differs from raw evidence.')
    return verified


def verify_current():
    return validate(json.loads((record.HERE/'results/applications-current.json').read_text()))


def render(summary):
    lines=['# Application integration results','','**All eight version profiles and native Linux pass. Readiness: Alpha.**',
        '', 'GORM 1.31.2 / shared SQL dialect 1.6.0; sqlx 1.4.0. All connections use the dedicated MariaDB driver.',
        '', 'Source: `'+summary['input_fingerprint']+'`. Correctness only; no performance measurement.',
        '', '| MariaDB | Go 1.25 PASS / FAIL / SKIP | Go 1.27 PASS / FAIL / SKIP |',
        '| --- | ---: | ---: |']
    def entry(key):
        profile=summary['profiles'][key];counts=profile['leaf_counts']
        return f'[✅ {counts["pass"]} / {counts["fail"]} / {counts["skip"]}]({profile["evidence_directory"]}/capture.json)'
    for server in SERVERS:
        lines.append('| '+summary['server_versions'][server].split('-MariaDB')[0]+' | '+
            ' | '.join(entry(f'go{go}-mariadb{server}') for go in COMPILERS)+' |')
    lines+=['', 'Native Linux / Go 1.27 / MariaDB 12.3: '+entry(NATIVE)+'.',
        '', 'Each profile requires race detection, both modules’ vet checks, exact module locks,',
        'a separate offline CGO-free core consumer, one owned server restart and resource cleanup.',
        'Required application cases cannot skip. TLS/compression workflows run on both transports.',
        '', '**Platform scope:** '+summary['platform_scope'],
        '', 'Coverage includes migrations (including a schema upgrade), CRUD/batches/upsert/RETURNING,',
        'exact decimals and unsigned values, UTF-8, JSON/BLOB/NULL/time, translated and original errors,',
        'automatic timestamps, savepoints/nested rollback, managed session reset, cached statements, pool exhaustion/cancellation,',
        'early Rows.Close, expiry, concurrent updates and committed/uncommitted state after restart.',
        '', 'These automated applications do not establish production workload, proxy/cluster failover,',
        'Windows, or other framework/version compatibility. User application pilots remain required.',
        '', '[Application guide](../docs/APPLICATIONS.md) · [Stage 8 and release checklist](../docs/STAGE8_APPLICATIONS.md) ·',
        '[Core correctness and historical timing](RESULTS.md) · [12-cell core matrix](COMPATIBILITY.md)',
        '', 'Run `python3 validation/verify.py` to require this evidence alongside the core suites.',
        'No benchmark or profiler was run for Stage 8. No package has been published or tagged.', '']
    return '\n'.join(lines)


def publish(directories):
    summary=build(directories);validate(summary)
    for path,content in ((record.HERE/'results/applications-current.json',json.dumps(summary,indent=2)+'\n'),
                         (record.HERE/'APPLICATIONS.md',render(summary))):
        temporary=path.with_suffix(path.suffix+'.tmp');temporary.write_text(content);temporary.replace(path)
    return summary


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--publish',action='store_true');parser.add_argument('--profile',type=Path,action='append',default=[])
    args=parser.parse_args();summary=publish(args.profile) if args.publish else verify_current()
    print(json.dumps({key:summary[key] for key in ('input_fingerprint','profile_count','performance_measured')},indent=2))


if __name__=='__main__':main()
