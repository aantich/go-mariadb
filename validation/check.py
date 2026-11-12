#!/usr/bin/env python3
"""Verify freshness and immutable baseline hashes without rerunning benchmarks."""
import json
from pathlib import Path
import sys
from run import HERE, PROFILE, digest, inputs


def main():
    capture=json.loads((HERE/'results/current.json').read_text())
    if not capture['complete'] or capture['inputs']!=inputs():
        raise SystemExit('Current results are incomplete or stale for the actual source/workloads.')
    if capture['capabilities']!=json.loads((HERE/'capabilities.json').read_text()):
        raise SystemExit('Capability inventory changed after the capture.')
    baseline=HERE/'baselines'/PROFILE
    for line in (baseline/'SHA256SUMS').read_text().splitlines():
        expected,name=line.split('  ',1)
        if Path(name).name!=name or digest((baseline/name).read_bytes())!=expected:
            raise SystemExit('Immutable baseline file changed: '+name)
    if any(len(row['samples'])!=7 for row in capture['benchmarks'].values()):
        raise SystemExit('Missing benchmark repetitions.')
    print('Fresh results; baseline checksums intact; seven repetitions per profile. Correctness exit:',capture['exit_code'])
    return 0


if __name__=='__main__':sys.exit(main())
