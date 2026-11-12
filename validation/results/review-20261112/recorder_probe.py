"""Read-only recorder review reproductions; no measured files are overwritten."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile

validation = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(validation))
import record

spec = importlib.util.spec_from_file_location('review_verify', validation/'verify.py')
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)
current = json.loads((validation/'results/current.json').read_text())
baseline = json.loads((validation/'baselines'/record.PROFILE/'capture.json').read_text())
print('Actual capture:', len(current['benchmarks']), 'profiles; inputs unchanged:', current['inputs'] == record.inputs())

# Supply a synthetic capture in an isolated directory. References remain the
# original read-only files; the actual live capture is never modified.
with tempfile.TemporaryDirectory(prefix='gomdb-review-recorder-') as directory:
    target = Path(directory)
    (target/'results').mkdir()
    (target/'capabilities.json').symlink_to(validation/'capabilities.json')
    (target/'baselines').symlink_to(validation/'baselines', target_is_directory=True)
    incomplete = json.loads(json.dumps(current))
    removed = 'BenchmarkQuery'
    del incomplete['benchmarks'][removed]
    (target/'results/current.json').write_text(json.dumps(incomplete))
    verify.HERE = target
    verify.main()
    print('BUG: verifier accepted', len(incomplete['benchmarks']), 'profiles with', removed, 'missing')

    # Regenerate only into this temporary directory to examine whether the
    # current explanatory report can be reproduced from its recorder.
    record.HERE = target
    record.render(current, baseline)
    rendered = (target/'RESULTS.md').read_text()
    print('Regenerated current overview present:', '## Current stage at a glance' in rendered)
    print('Regenerated over-broad workload caption present:', 'Read operations consume 1,000 rows' in rendered)
