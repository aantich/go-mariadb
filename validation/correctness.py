#!/usr/bin/env python3
"""Run and publish correctness independently of historical performance samples."""
import argparse
from collections import Counter
import json
import os
from pathlib import Path
import subprocess
import sys

import ci
import record
from contracts import verified_checks
from provenance import validate_server
import pass_history


def leaves(records):
    """Subtest names may contain several slashes without intermediate events."""
    parents = set()
    for key in records:
        parent = key.rpartition('/')[0]
        while parent:
            parents.add(parent)
            parent = parent.rpartition('/')[0]
    return {key:value for key,value in records.items() if key not in parents}


def validate(capture, directory):
    if capture.get('exit_code') or not capture.get('inputs_stable') or capture.get('inputs') != record.inputs():
        raise ValueError('Correctness capture failed, incomplete or stale.')
    if capture.get('input_fingerprint') != record.digest(json.dumps(capture['inputs'],sort_keys=True).encode()):
        raise ValueError('Incorrect correctness source fingerprint.')
    verified_checks(capture)
    if capture.get('capabilities') != json.loads((record.HERE/'capabilities.json').read_text()):
        raise ValueError('Capability inventory changed after correctness checks.')
    for name, expected in capture.get('logs', {}).items():
        if Path(name).name != name or record.digest((directory/name).read_bytes()) != expected:
            raise ValueError('Correctness log checksum mismatch: '+name)
    if set(capture.get('logs',{})) != {'correctness.log','recorder.log','vet.log','server-before.log','server-after.log'}:
        raise ValueError('Correctness log set is incomplete.')
    validate_server(capture, directory)
    issues, records = ci.assess_tests((directory/'correctness.log').read_text(),
                                     capture['stages']['correctness']['exit_code'],
                                     capture.get('instrumentation_required',False))
    if issues or records != capture['stages']['correctness']['cases']:
        raise ValueError('Raw correctness events do not match the captured assertions.')
    pass_history.validate(capture, directory, {name:case.get('status') for name,case in records.items()})
    return records


def summarize(capture, directory):
    records = validate(capture, directory)
    reference = json.loads((record.HERE/'results/current.json').read_text())
    statuses = {record.case_key(key):value.get('status') for key,value in records.items()}
    lost = [case['name'] for suite in ('upstream','protocol','mariadb')
            for case in reference['stages'][suite]['cases'] if case.get('status') == 'pass'
            and statuses.get(record.case_key(case['name'])) != 'pass']
    if lost:
        raise ValueError('Prior passing assertions lost: '+', '.join(lost))
    for path in (record.HERE/'baselines').iterdir():
        if (path/'SHA256SUMS').exists() and not record.reference_hashes_match(path):
            raise ValueError('Immutable reference checksum mismatch: '+str(path))
    original = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
    if not record.baseline_workloads_match(original):
        raise ValueError('Frozen workloads differ beyond approved identity/contract migrations.')
    unique = leaves(records)
    return {**capture, 'verified_checks':verified_checks(capture), 'leaf_counts':dict(Counter(case['status'] for case in unique.values())),
            'prior_passes_lost':lost, 'evidence_directory':str(directory.resolve().relative_to(record.HERE.resolve())),
            'benchmark_capture_fingerprint':reference['input_fingerprint'], 'performance_measured':False}


def publish(capture, directory):
    summary = summarize(capture, directory)
    (record.HERE/'results/correctness-current.json').write_text(json.dumps(summary,indent=2)+'\n')
    previous = json.loads((record.HERE/'results/current.json').read_text())
    baseline = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
    record.render(previous,baseline,summary)
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config',type=Path,default=record.HERE/'.local/config.json')
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--publish',action='store_true')
    parser.add_argument('--publish-existing',action='store_true',help='Verify a completed exact-source capture; do not rerun checks.')
    args = parser.parse_args()
    if not args.publish_existing:
        if (args.output/'capture.json').exists():
            raise SystemExit('Retain previous attempts; use a new output directory.')
        env = os.environ.copy()
        env['MARIADB_VALIDATION_CONFIG'] = str(args.config.resolve())
        command = [sys.executable,str(record.HERE/'ci.py'),'--correctness-only','--output',str(args.output)]
        code = subprocess.run(command,cwd=record.ROOT,env=env).returncode
        if code:
            return code
    capture = json.loads((args.output/'capture.json').read_text())
    summary = publish(capture,args.output) if args.publish else summarize(capture,args.output)
    print(json.dumps({key:summary[key] for key in ('input_fingerprint','leaf_counts','prior_passes_lost','performance_measured')},indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
