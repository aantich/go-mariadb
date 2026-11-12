#!/usr/bin/env python3
"""Verify current dedicated-driver inputs and every immutable baseline artifact."""
import json
from pathlib import Path
from record import HERE, PROFILE, NATIVE_BASELINE, baseline_workloads_match, digest, inputs, native_workloads_match, reference_hashes_match
from evidence import benchmark_issues


def main():
    current = json.loads((HERE/'results/current.json').read_text())
    if not current['complete'] or current['inputs'] != inputs():
        raise SystemExit('Current measurement inputs are stale or incomplete.')
    if current['capabilities'] != json.loads((HERE/'capabilities.json').read_text()):
        raise SystemExit('Capability inventory changed after measurement.')
    baseline_dir = HERE/'baselines'/PROFILE
    baseline = json.loads((baseline_dir/'capture.json').read_text())
    for line in (baseline_dir/'SHA256SUMS').read_text().splitlines():
        expected, name = line.split('  ', 1)
        if Path(name).name != name or digest((baseline_dir/name).read_bytes()) != expected:
            raise SystemExit('Immutable baseline changed: '+name)
    if not baseline_workloads_match(baseline):
        raise SystemExit('Original workload bodies changed beyond verified identity metadata.')
    for directory in (HERE/'baselines').iterdir():
        if (directory/'SHA256SUMS').exists() and not reference_hashes_match(directory):
            raise SystemExit('Immutable reference checksum mismatch: '+str(directory))
    references = [baseline]
    if NATIVE_BASELINE.exists():
        native = json.loads((NATIVE_BASELINE/'capture.json').read_text())
        references.append(native)
        if not native_workloads_match(native, current['inputs']):
            raise SystemExit('Native workload changed after its first reference.')
    issues = benchmark_issues(current['benchmarks'], references)
    if issues:
        raise SystemExit('\n'.join(issues))
    if current.get('lost_baseline_passes'):
        raise SystemExit('Original passing assertions were lost.')
    print('Fresh dedicated-driver results; baseline checksums and original workload bodies intact.')
    print('Seven repetitions per profile. Correctness exit:', current['exit_code'])


if __name__ == '__main__':
    main()
