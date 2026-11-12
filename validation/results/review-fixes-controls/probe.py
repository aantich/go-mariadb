"""Interleaved CPU diagnostics, separate from accepted seven-sample captures."""
from pathlib import Path
import hashlib
import json
import os
import statistics
import subprocess
import sys

VALIDATION = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(VALIDATION))
import record

root = VALIDATION.parent
out = Path(__file__).resolve().parent
variants = {'original':Path('/private/tmp/go-mariadb-original-control-20261112/original-root.test'),
            'before-review':root/'validation/results/runs/20261111T231601152353Z/upstream.test',
            'corrected':root/'validation/results/runs/20261112T082038200863Z/upstream.test'}
pattern = '^BenchmarkRowsScan$/^int64$/^binary=(false|true)$/^rows=100$/^direct=(false|true)$'
env = os.environ.copy()
env['GOMAXPROCS'] = '4'
orders = [['original','before-review','corrected'], ['corrected','before-review','original'], ['before-review','original','corrected']]
capture = {'purpose':'Diagnostic CPU controls; never replace full capture or references', 'complete':False,
           'pattern':pattern, 'benchtime':'500ms', 'conditions':'No idle confirmation. Order fixed before measuring; all outcomes retained.',
           'initial_attempt':'attempt0-original.log: all four scan cases passed; diagnostic incorrectly expected a fifth top-level DSN case under a nested benchmark filter. That expectation was corrected before this complete control.',
           'source_fingerprint':record.digest(json.dumps(record.inputs(), sort_keys=True).encode()),
           'binaries':{name:hashlib.sha256(path.read_bytes()).hexdigest() for name,path in variants.items()}, 'samples':[]}
for repetition, order in enumerate(orders):
    for name in order:
        print('CPU control', repetition, name, flush=True)
        result = subprocess.run([str(variants[name]), '-test.run=^$', '-test.bench='+pattern, '-test.benchtime=500ms', '-test.benchmem', '-test.count=1'],
                                cwd=root, env=env, text=True, capture_output=True, timeout=90)
        output = result.stdout+result.stderr
        (out/f'{repetition}-{name}.log').write_text(output)
        sample = {'repetition':repetition, 'variant':name, 'exit_code':result.returncode, 'benchmarks':record.benchmark_rows(output)}
        capture['samples'].append(sample)
        (out/'capture.json').write_text(json.dumps(capture, indent=2)+'\n')
        if result.returncode or len(sample['benchmarks']) != 4:
            raise SystemExit('Unexpected diagnostic outcome retained in capture/logs.')
summary = {}
for benchmark in capture['samples'][0]['benchmarks']:
    summary[benchmark] = {name:statistics.median(s['benchmarks'][benchmark]['ns/op'] for s in capture['samples'] if s['variant']==name) for name in variants}
    summary[benchmark]['corrected_vs_before_percent'] = 100*(summary[benchmark]['corrected']/summary[benchmark]['before-review']-1)
capture['summary'] = summary
capture['inputs_stable'] = capture['source_fingerprint'] == record.digest(json.dumps(record.inputs(), sort_keys=True).encode())
capture['complete'] = capture['inputs_stable']
(out/'capture.json').write_text(json.dumps(capture, indent=2)+'\n')
print(json.dumps(summary, indent=2))
