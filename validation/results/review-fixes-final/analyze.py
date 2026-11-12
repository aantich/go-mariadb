"""Retain the completed correction capture without changing accepted references."""
from collections import Counter
import gzip
import hashlib
import json
from pathlib import Path
import re
import shutil
import sys
import tempfile
from unittest import mock

VALIDATION = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(VALIDATION))
import record


def main(run):
    output = Path(__file__).resolve().parent
    current = json.loads((run/'capture.json').read_text())
    if not current['complete'] or current['inputs'] != record.inputs():
        raise SystemExit('Cannot archive incomplete or stale correction evidence.')
    original = json.loads((VALIDATION/'baselines'/record.PROFILE/'capture.json').read_text())
    linux = json.loads((VALIDATION/'results/review-fixes-linux-final/capture.json').read_text())
    if linux['inputs'] != current['inputs'] or not linux['inputs_stable'] or linux['exit_code']:
        raise SystemExit('Linux verification does not cover the final exact inputs.')
    changes = {name:100*(row['median']['ns/op']/original['benchmarks'][name]['median']['ns/op']-1)
               for name,row in current['benchmarks'].items() if name in original['benchmarks']}
    suites = {}
    for name in ('upstream','protocol','mariadb','race'):
        cases = current['stages'][name]['cases']
        suites[name] = dict(Counter(case.get('status','incomplete') for case in cases))
        suites[name]['failures'] = [case['name'] for case in cases if case.get('status') == 'fail']
    fuzz = {}
    for name in ('fuzz','fuzz-terminator'):
        log = gzip.open(run/(name+'.log.gz'), 'rt').read()
        matches = re.findall(r'execs:\s*(\d+)', log)
        fuzz[name] = {'exit_code':current['stages'][name]['exit_code'], 'executions':int(matches[-1]) if matches else None}
    native = {}
    for count in (1,16,100,1000):
        values = {method:current['benchmarks'][f'BenchmarkMariaDBNativeBulk/{count}/{method}']['median']['ns/op']/1e6
                  for method in ('prepared','native_bulk','text_batch','local_infile')}
        values['prepared_over_native'] = values['prepared']/values['native_bulk']
        native[str(count)] = values
    with tempfile.TemporaryDirectory() as directory, mock.patch.object(record, 'HERE', Path(directory)):
        record.render(current, original)
        generated = (Path(directory)/'RESULTS.md').read_bytes()
    actual = (VALIDATION/'RESULTS.md').read_bytes()
    if generated != actual:
        raise SystemExit('The live report does not match its generated evidence.')
    analysis = {'source_fingerprint':current['input_fingerprint'], 'recorded_at':current['recorded_at'],
                'complete':current['complete'], 'exit_code':current['exit_code'], 'suites':suites,
                'profiles':len(current['benchmarks']), 'samples_per_profile':sorted({len(row['samples']) for row in current['benchmarks'].values()}),
                'benchmark_issues':current['benchmark_issues'], 'lost_baseline_passes':current['lost_baseline_passes'],
                'conditions':current['conditions'], 'fuzz':fuzz, 'native_alternatives_ms':native,
                'timing_signals':{'over_5_percent':sum(c > 5 for c in changes.values()),
                                  'over_20_percent':sum(c > 20 for c in changes.values()),
                                  'faster_over_5_percent':sum(c < -5 for c in changes.values())},
                'original_timing_changes_percent':changes,
                'linux_exact_inputs':True, 'linux_ci_policy_exit':linux['exit_code'],
                'race_warnings':gzip.open(run/'race.log.gz','rt').read().count('WARNING: DATA RACE'),
                'report_reproduced_exactly':True, 'report_sha256':hashlib.sha256(actual).hexdigest()}
    controls = VALIDATION/'results/review-fixes-controls/capture.json'
    if controls.exists():
        diagnostic = json.loads(controls.read_text())
        analysis['diagnostic_controls'] = {'path':'../review-fixes-controls/capture.json',
            'complete':diagnostic['complete'], 'source_fingerprint':diagnostic['source_fingerprint'],
            'corrected_vs_before_percent':{name:values['corrected_vs_before_percent'] for name,values in diagnostic['summary'].items()}}
    shutil.copy2(run/'capture.json', output/'capture.json')
    for path in run.glob('*.log.gz'):
        shutil.copy2(path, output/path.name)
    (output/'analysis.json').write_text(json.dumps(analysis, indent=2)+'\n')
    (output/'SHA256SUMS').write_text(''.join(record.digest(path.read_bytes())+'  '+path.name+'\n'
        for path in sorted(output.iterdir()) if path.is_file() and path.name != 'SHA256SUMS'))
    print(json.dumps({key:value for key,value in analysis.items() if key != 'original_timing_changes_percent'}, indent=2))


if __name__ == '__main__':
    main(Path(sys.argv[1]).resolve())
