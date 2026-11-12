#!/usr/bin/env python3
"""Record the unchanged-driver baseline and comparable subsequent Go runs.

Preflight a real MariaDB before executing tests. Keep ordinary failures, skips,
missing APIs, commands, source/workload hashes and every benchmark repetition.
Timing runs never overlap correctness/race builds. No command commits or pushes.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import re
import shutil
import statistics
import subprocess
import sys
from evidence import benchmark_issues
from contracts import FULL_CHECK_ENVIRONMENT, FULL_COMMANDS, measurement_commands
import full_capture
from provenance import read_server, server_command
from stage4_workloads import STAGE4_PROFILES, STAGE4_WORKLOAD_INPUTS, stage4_issues
import pass_history

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
PROFILE = 'macos-arm64-mariadb-12.3.3-go1.27.4'
NATIVE_BASELINE = HERE/'baselines'/('native-bulk-stage1-'+PROFILE)
STAGE4_BASELINE = HERE/'baselines'/('native-stage4-'+PROFILE)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def legacy_inputs():
    tracked = subprocess.check_output(['git', 'ls-files'], cwd=ROOT, text=True).splitlines()
    driver = {p: digest((ROOT/p).read_bytes()) for p in tracked
              if (p.endswith('.go') and not p.endswith('_test.go')) or p in ['go.mod', 'go.sum']}
    workload = {p: digest((ROOT/p).read_bytes()) for p in tracked if p.endswith('_test.go')}
    paths = [ROOT/'mariadb_validation_protocol_test.go', *HERE.glob('*.go'), *HERE.glob('*.py')]
    workload.update({str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in paths if p.exists()})
    return {'driver': driver, 'workload': workload}


def inputs():
    # Include untracked production additions, not only git ls-files.
    files = [p for p in ROOT.rglob('*.go') if not any(x in p.parts for x in ('results','baselines','.git'))]
    driver = {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in files if not p.name.endswith('_test.go')}
    driver.update({name:digest((ROOT/name).read_bytes()) for name in ('go.mod','go.sum')})
    # The optional native provider is executable driver code too. A Go-only
    # fingerprint would miss ABI, mechanism-policy and cleanup changes here.
    driver.update({str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in (ROOT/'gssapi').glob('native.*')
                   if p.suffix in ('.c','.h')})
    workload = {str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in files if p.name.endswith('_test.go')}
    workload.update({str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in HERE.glob('*.py')})
    workload.update({str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in (HERE/'auth_fixture').glob('*') if p.is_file()})
    workload['validation/identity-migration.json'] = digest((HERE/'identity-migration.json').read_bytes())
    workload['validation/contract-migrations.json'] = digest((HERE/'contract-migrations.json').read_bytes())
    for path in ('.github/workflows/test.yml', '.github/workflows/actions.lock'):
        workload[path] = digest((ROOT/path).read_bytes())
    return {'driver':dict(sorted(driver.items())), 'workload':dict(sorted(workload.items()))}


def original_workload_bytes(path):
    # Only mechanical package/import/registration metadata may differ. Reversing
    # these exact substitutions must reproduce the original SHA-256, byte-for-byte.
    data = (ROOT/path).read_bytes()
    if path.endswith('.go'):
        data = data.replace(b'package mariadb_test\n',b'package mysql_test\n').replace(b'package mariadb\n',b'package mysql\n')
        old_import = b'mysql "github.com/go-sql-driver/mysql"' if path.startswith('validation/') else b'"github.com/go-sql-driver/mysql"'
        data = data.replace(b'mysql "github.com/aantich/go-mariadb"',old_import)
        data = data.replace(b'github.com/aantich/go-mariadb',b'github.com/go-sql-driver/mysql')
        data = data.replace(b'sql.Open("mariadb",',b'sql.Open("mysql",')
        if path == 'statement_test.go':
            data = data.replace(b'unsupported type mariadb.derived',b'unsupported type mysql.derived')
    # The sole approved live contract change requests managed session reuse.
    # Reverse exact bytes; any assertion/body change still fails the frozen hash.
    for change in json.loads((HERE/'contract-migrations.json').read_text())['changes']:
        if change['path'] == path:
            before, after = change['before'].encode(), change['after'].encode()
            if data.count(after) != 1:
                return data
            data = data.replace(after, before, 1)
    return data


def baseline_workloads_match(baseline):
    return all(digest(original_workload_bytes(p)) == sha for p,sha in baseline['inputs']['workload'].items())


def case_key(name):
    return name.replace('github.com/aantich/go-mariadb','github.com/go-sql-driver/mysql')


def native_workloads_match(reference, actual_inputs):
    """New API timings are comparable only while their own workload is frozen."""
    frozen = reference.get('native_workload_inputs', {})
    return bool(frozen) and all(actual_inputs['workload'].get(p) == sha for p,sha in frozen.items())


def reference_hashes_match(directory):
    """Reject modified references before using any number in a comparison."""
    lines = (directory/'SHA256SUMS').read_text().splitlines()
    if not lines: return False
    contains_capture = False
    for line in lines:
        expected,name = line.split('  ',1)
        contains_capture |= name == 'capture.json'
        if Path(name).name != name or digest((directory/name).read_bytes()) != expected:
            return False
    return contains_capture


def checkpoint(directory, state, progress):
    """An interrupted command retains provenance, never a completed live report."""
    partial = {**state, 'complete': False, 'in_progress': progress}
    (directory/'capture.partial.json').write_text(json.dumps(partial, indent=2)+'\n')


def test_cases(text, include_containers=False):
    """Count leaves once; use all events when protecting historical identities."""
    records = {}
    for line in text.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not event.get('Test'):
            continue
        key = event['Package'] + '/' + event['Test']
        item = records.setdefault(key, {'name': key, 'output': ''})
        if event['Action'] == 'output':
            item['output'] += event.get('Output', '')
        if event['Action'] in ['pass', 'fail', 'skip']:
            item['status'] = event['Action']
    if include_containers:
        return list(records.values())
    parents = set()
    for key in records:
        parent = key.rsplit('/', 1)[0]
        while parent:
            parents.add(parent)
            parent = parent.rpartition('/')[0]
    return [value for key,value in sorted(records.items()) if key not in parents]


def lost_reference_passes(references, observed):
    """Protect every old PASS, even ancestors counted by a historical parser.

    Summary counts deliberately exclude containers. Regression protection must
    instead inspect their real raw statuses, not mistake filtering for removal.
    Children remain independently protected and process failures still fail runs.
    """
    lost = []
    for reference in references:
        for name in ('upstream', 'protocol', 'mariadb'):
            statuses = {case_key(case['name']):case.get('status') for case in observed.get(name, [])}
            lost += [case['name'] for case in reference['stages'][name]['cases']
                     if case.get('status') == 'pass' and statuses.get(case_key(case['name'])) != 'pass']
    return sorted(set(lost))


def benchmark_rows(text):
    rows = {}
    for line in text.splitlines():
        match = re.match(r'^(Benchmark\S+)-(\d+)\s+(\d+)\s+(.+)$', line)
        if not match:
            continue
        metrics = match[4].split()
        row = {'iterations': int(match[3]), 'gomaxprocs': int(match[2])}
        for value, unit in zip(metrics[::2], metrics[1::2]):
            row[unit] = float(value)
        rows[match[1]] = row
    return rows


def render(current, baseline, correctness=None):
    features = json.loads((NATIVE_BASELINE/'capture.json').read_text())['benchmarks'] if NATIVE_BASELINE.exists() else {}
    if STAGE4_BASELINE.exists():
        features = {**features, **json.loads((STAGE4_BASELINE/'capture.json').read_text())['benchmarks']}
    latest = []
    if correctness is not None:
        from contracts import verified_checks
        checks = verified_checks(correctness)
        counts = correctness['leaf_counts']
        server = correctness['server']['before']
        checked = [label for key,label in (('race','Race detection'),('vet','vet'),('harness_policy','harness policy checks')) if checks[key]]
        latest = ['## Latest correctness — native MariaDB APIs', '',
                  f"Recorded: **{correctness['recorded_at']}** · Source: `{correctness['input_fingerprint']}` · **{counts.get('pass',0)} PASS / {counts.get('fail',0)} FAIL / {counts.get('skip',0)} SKIP**, unique leaves.",
                  f"Server: **{server['version']}** · Go: `{correctness['go']}` · Host: `{correctness['host']}` · GOMAXPROCS: **{correctness['check_environment']['GOMAXPROCS']}** · performance_schema: **{server['performance_schema']}**. Server settings match before/after checks.",
                  ', '.join(checked)+' pass. No prior passing assertion was lost. Managed isolation is explicitly opted in; ordinary checkout remains liveness-only.',
                  f"[Raw correctness receipt]({correctness['evidence_directory']}/capture.json) · [Reusable statements](../docs/STAGE4_STATEMENT_REUSE.md) · [Native API contracts](../docs/STAGE3_NATIVE_APIS.md) · [Session foundations](../docs/STAGE2_SESSION_FOUNDATIONS.md).", '',
                  '**No performance measurements were made for this source.** The timing tables and full-capture failures below belong to the earlier source fingerprint shown there. They are historical, qualified measurements; none were copied into a new baseline or presented as current-source timings.', '',
                  '<details><summary>Earlier full-capture results and historical failures</summary>', '',
                  '## Earlier full performance capture', '']
    lines = ['# Go MariaDB live validation and benchmarks', '',
             '> Dedicated MariaDB driver, Alpha. Baseline = unchanged upstream; Current = the measured working tree below.', '',
             f"Recorded: **{current['recorded_at']}** · Parent commit: `{current['commit'][:12]}` · Go: `{current['go']}`", '',
             f"Inputs: `{current.get('input_fingerprint','historical')}` · Matches committed source: **{current['driver_matches_commit']}**. See capture JSON for the complete dirty tree and per-file hashes.", '',
             f"Server: **{current['server']['version']}** · Host: `{current['host']}` · GOMAXPROCS: **4**.", '',
             f"Benchmark conditions: **{current['conditions']}**. Seven independent processes per benchmark family; medians and raw repetitions are retained.", '',
             *overview(current, baseline),
             '## Correctness', '', '| Suite | PASS | FAIL | SKIP | Exit |', '| --- | ---: | ---: | ---: | ---: |']
    if latest:
        lines[2:2] = latest
        lines = [line.replace('## Current stage at a glance','## Earlier measured stage at a glance').replace('## Correctness','## Earlier full-capture correctness') for line in lines]
    for name,stage in current['stages'].items():
        counts=Counter(case.get('status','incomplete') for case in stage.get('cases',[]))
        lines.append(f"| {name} | {counts['pass']} | {counts['fail']} | {counts['skip']} | {stage['exit_code']} |")
    lines += ['', 'Historical totals retain the parser used by that capture; latest correctness excludes all ancestor containers. Race cases repeat coverage.' if correctness else 'Leaf cases are counted once. Race cases repeat correctness coverage; do not add them to the ordinary total. Vet/build rows have no individual test cases.', '',
              '## Earlier full-capture failures' if correctness else '## Failed assertions', '',
              '| Suite | Baseline failures | Earlier capture failures |' if correctness else '| Suite | Baseline failures | Current failures |', '| --- | ---: | ---: |']
    for name in ('upstream','protocol','mariadb'):
        old=sum(c.get('status')=='fail' for c in baseline['stages'][name].get('cases',[]))
        now=sum(c.get('status')=='fail' for c in current['stages'][name].get('cases',[]))
        lines.append(f'| {name} | {old} | {now} |')
    lines.append('')
    for name,stage in current['stages'].items():
        failures=[c for c in stage.get('cases',[]) if c.get('status')=='fail']
        if failures:
            lines += [f'<details><summary>{name}: {len(failures)} failed assertions</summary>', '']
            for case in failures:
                lines.append('- `' + case['name'] + '`')
            lines += ['', '</details>', '']
    if correctness:
        lines += ['</details>', '']
    lines += ['## Current capability inventory' if correctness else '## Capability inventory', '', '| Capability | Baseline | Current | Evidence / next step |', '| --- | --- | --- | --- |']
    old_capabilities={item['name']:item['status'] for item in baseline['capabilities']}
    for item in (correctness or current)['capabilities']:
        lines.append(f"| {item['name']} | {old_capabilities.get(item['name'],'new inventory')} | {item['status']} | {item['evidence']} |")
    lines += ['', 'Inventory entries are source-reviewed API gaps, not passing feature tests. Missing external services are recorded separately from missing APIs. See [coverage and limits](COVERAGE.md).', '',
              '## Benchmark comparison', '',
              f"Last measured source: `{current.get('input_fingerprint','historical')}`. Current source has **no performance measurements**." if correctness else '', '',
              '**Stage 4: 62 new profiles are UNMEASURED.** No benchmark or profiling workload ran during this implementation. Their first reference is pending an authorized measurement window; the 200-profile historical capture below is unchanged. See [workload units and limits](../docs/STAGE4_STATEMENT_REUSE.md#additive-benchmark-workloads-still-unmeasured).' if not set(STAGE4_PROFILES) <= current['benchmarks'].keys() else 'Stage 4 profiles use a separate first reference; their first measurements do not establish an improvement over the original driver.', '',
              'Baseline = immutable unchanged-upstream capture for original workloads; [first native-bulk capture](baselines/native-bulk-stage1-macos-arm64-mariadb-12.3.3-go1.27.4/capture.json) for the new matched bulk profiles. Current = latest complete capture. Change = `(Current / Baseline - 1) × 100`; negative means less time. Units are **ns/operation**. The original added live reads consume 1,000 rows, BLOB reads consume 1 MiB, and original added writes replace 100 rows transactionally. Native bulk alternatives use **1/16/100/1000 rows per transaction**. Inherited workloads retain their own row counts and decoder/scanner units; do not apply the live-read row count to every inherited benchmark. Connection operations establish and close one connection.', '',
              'When present, the independent Stage 4 reference is `baselines/native-stage4-'+PROFILE+'/capture.json`. Stage 4 bulk uses 1/16/100/1,000 rows, uploads 64 KiB/1 MiB/16 MiB, cursor reads 1,000/10,000 rows, ALTER progress two DDL commands plus verification, and quiet progress one command per operation. See the operation metrics in raw samples.', '',
              '| Benchmark | Baseline ns/op | Last measured ns/op | Change vs baseline | Last measured B/op | Last measured allocs/op |' if correctness else '| Benchmark | Baseline ns/op | Current ns/op | Change vs baseline | Current B/op | Current allocs/op |',
              '| --- | ---: | ---: | ---: | ---: | ---: |']
    for name in sorted(current['benchmarks'].keys() | baseline['benchmarks'].keys() | features.keys()):
        row = current['benchmarks'].get(name)
        old=baseline['benchmarks'].get(name) or features.get(name)
        before=old['median']['ns/op'] if old else None
        if row is None:
            lines.append(f'| `{name}` | {before:,.1f} | **MISSING** | incomplete | — | — |')
            continue
        now=row['median']['ns/op']
        change=f'{(now/before-1)*100:+.1f}%' if before else 'new workload'
        lines.append(f"| `{name}` | {before:,.1f} | {now:,.1f} | {change} | {row['median'].get('B/op',0):,.0f} | {row['median'].get('allocs/op',0):,.0f} |" if before else f"| `{name}` | — | {now:,.1f} | first measurement | {row['median'].get('B/op',0):,.0f} | {row['median'].get('allocs/op',0):,.0f} |")
    lines += ['', 'These are Go-only measurements, not a speed comparison with the Haskell driver or Connector/C. Metadata controls use identical SQL, scanner and network-accounting wrappers; only the advertised metadata-cache capability changes.', '',
              '## Reproduce and preserve history', '',
              'See [validation instructions](README.md), [review findings](../docs/MARIADB_REVIEW.md) and [implementation roadmap](../docs/ROADMAP.md).', '',
              'Raw logs, hashes, process repetitions and server settings are in the capture JSON and compressed baseline logs. A nonzero full-run exit is retained when correctness fails; benchmark completion does not turn it into a pass.', '']
    (HERE/'RESULTS.md').write_text('\n'.join(lines))


def overview(current, baseline):
    """Generate explanatory sections from evidence, so a new run cannot erase them."""
    cases = [case for name in ('upstream','protocol','mariadb') for case in current['stages'][name].get('cases', [])]
    counts = Counter(case.get('status', 'incomplete') for case in cases)
    changes = [row['median']['ns/op']/baseline['benchmarks'][name]['median']['ns/op']-1
               for name,row in current['benchmarks'].items() if name in baseline['benchmarks']]
    session_gap = any(case['name'].endswith('/TestMariaDBDesiredCleanPoolReset') and case.get('status') == 'fail' for case in cases)
    lines = ['## Current stage at a glance', '',
             f"🟢 **{counts['pass']} passing assertions** · 🔴 **{counts['fail']} failed assertions** · ⏭ **{counts['skip']} skips**.",
             'Ordinary totals exclude repeated race checks. ' + ('The desired clean-session assertion remains an API gap; other failures are listed below.' if session_gap else 'Clean session reuse is explicitly opted in; ordinary pool checkout remains liveness-only.'),
             f"The complete workflow exits **{current['exit_code']}**. Readiness remains **Alpha**; automated coverage does not establish production application readiness.", '',
             f"All **{len(current['benchmarks'])} benchmark profiles** require **seven independent process repetitions**. Original passing assertions lost: **{len(current.get('lost_baseline_passes', []))}**.",
             f"**Performance acceptance remains open:** {sum(c > .05 for c in changes)} original profiles exceed +5% time, {sum(c > .20 for c in changes)} exceed +20%, and {sum(c < -.05 for c in changes)} improve by more than 5%.",
             'These are investigation thresholds, not proof of a code-caused change. Measurement conditions and per-family host observations remain in the capture.', '',
             '| Rows per transaction | Prepared loop, current ms | Native bulk, current ms | Prepared/native throughput ratio | Text batch, current ms | LOCAL INFILE, current ms |',
             '| --- | ---: | ---: | ---: | ---: | ---: |']
    for rows in (1,16,100,1000):
        names = [f'BenchmarkMariaDBNativeBulk/{rows}/{method}' for method in ('prepared','native_bulk','text_batch','local_infile')]
        if all(name in current['benchmarks'] for name in names):
            prepared,native,text,infile = [current['benchmarks'][name]['median']['ns/op']/1e6 for name in names]
            lines.append(f'| {rows} | {prepared:.3f} | {native:.3f} | {prepared/native:.2f}× | {text:.3f} | {infile:.3f} |')
        else:
            lines.append(f'| {rows} | **MISSING** | **MISSING** | incomplete | — | — |')
    lines += ['', 'These compare matched API alternatives on the current driver. Text/TSV data and parameter matrices are prebuilt; native encoding remains timed. Every alternative includes the same transaction, deletion and count/sum/content verification. Native bulk includes transient prepare/close; the prepared control reuses its handle. These are not universal fork-versus-upstream speedups.', '',
              'The [review corrections](../docs/STAGE1_REVIEW_FIXES.md), [implementation record](../docs/DEDICATED_DRIVER.md) and [coverage limits](COVERAGE.md) describe supported paths and remaining work. Original references and earlier captures remain unchanged.', '']
    return lines


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config',type=Path,default=HERE/'.local/config.json')
    parser.add_argument('--freeze-baseline',action='store_true')
    parser.add_argument('--correctness-only',action='store_true')
    parser.add_argument('--freeze-native-baseline',action='store_true')
    parser.add_argument('--freeze-stage4-baseline',action='store_true',help='Freeze the first complete Stage 4 measurements; never permitted with --correctness-only.')
    parser.add_argument('--conditions',required=True,help='Observed/confirmed host conditions; never infer idle from silence')
    args=parser.parse_args()
    if args.freeze_stage4_baseline and (args.correctness_only or STAGE4_BASELINE.exists()):
        raise SystemExit('Stage 4 reference already exists or measurement is disabled; refusing freeze.')
    if args.freeze_native_baseline and NATIVE_BASELINE.exists(): raise SystemExit('Native baseline already exists; refusing overwrite.')
    for reference in (HERE/'baselines').iterdir():
        if (reference/'SHA256SUMS').exists() and not reference_hashes_match(reference):
            raise SystemExit('Immutable reference checksum mismatch: '+str(reference))
    cfg=json.loads(args.config.read_text())
    if cfg['database']!='go_mariadb_validation' or cfg['upstream_database']!='go_mariadb_upstream':
        raise SystemExit('Refusing non-reserved database names.')
    baseline_dir=HERE/'baselines'/PROFILE
    if args.freeze_baseline and baseline_dir.exists():
        raise SystemExit('Baseline already exists: refusing overwrite.')
    now=datetime.now(timezone.utc)
    run=HERE/'results'/'runs'/now.strftime('%Y%m%dT%H%M%S%fZ')
    run.mkdir(parents=True)
    # Keep the previous receipt before current.json can be replaced. Immutable
    # upstream references predate many API tests and cannot protect those names.
    shutil.copy2(HERE/'results/current.json',run/'prior-capture.json')
    env=os.environ.copy()
    env.update(GOCACHE='/private/tmp/go-mariadb-validation-20261111/go-cache',
               MYSQL_TEST_USER=cfg['user'],MYSQL_TEST_PASS=cfg['password'],
               MYSQL_TEST_ADDR=f"{cfg['host']}:{cfg['port']}",MYSQL_TEST_DBNAME=cfg['upstream_database'],
               MARIADB_VALIDATION_CONFIG=str(args.config.resolve()))
    env.update(FULL_CHECK_ENVIRONMENT)
    # Inherited concurrency stress deliberately exhausts @@max_connections.
    # Keep it disabled on a shared service; the new suite tests a bounded pool.
    env.pop('MYSQL_TEST_CONCURRENT',None)
    state = None
    def command(name,argv):
        print(name,flush=True)
        if state is not None:
            checkpoint(run, state, {'command': name, 'argv': argv})
        p=subprocess.run(argv,cwd=ROOT,env=env,text=True,capture_output=True,timeout=600)
        output=(p.stdout+p.stderr).replace(cfg['password'],'[redacted]')
        with gzip.open(run/(name+'.log.gz'),'wt') as f:f.write(output)
        return p.returncode,output
    client = shutil.which('mariadb') or shutil.which('mysql')
    if not client:
        raise SystemExit('MariaDB client is required for read-only endpoint provenance.')
    endpoint = {key:cfg[key] for key in ('host','port','user','database')}
    server_argv = server_command(endpoint, client)
    before=inputs()
    state={'schema':1,'recorded_at':now.isoformat(),'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
           'go':subprocess.check_output(['go','version'],env=env,text=True).strip(),'host':platform.platform(),'conditions':args.conditions,
           'validation_contract':full_capture.CONTRACT, 'check_environment':dict(FULL_CHECK_ENVIRONMENT),
           'server_provenance':{'endpoint':endpoint, 'command':server_argv},
           'instrumentation_required':env.get('GO_MARIADB_REQUIRE_INSTRUMENTATION') == '1',
           'execution_directory':str(run.resolve()),
           'evidence_directory':str(Path('results')/'full'/run.name),
           'inputs':before,'stages':{},'benchmarks':{},'capabilities':json.loads((HERE/'capabilities.json').read_text()),
           'working_tree':subprocess.check_output(['git','status','--short'],cwd=ROOT,text=True),
           'host_observations':[],
           'hardware':subprocess.check_output(['sysctl','-n','machdep.cpu.brand_string','hw.ncpu','hw.memsize'],text=True).splitlines(),
           'method':{'process_repetitions':7,'benchtime':'200ms','gomaxprocs':4,'warmups':'original forty added serial profiles: two explicit warmups; native bulk, Stage 4 and inherited/parallel profiles: Go benchmark calibration','profile':PROFILE}}
    if env.get('GO_MARIADB_AUTH_RECEIPT'):
        state['external_auth'] = json.loads(env['GO_MARIADB_AUTH_RECEIPT'])
    def server_snapshot(phase):
        try:
            snapshot, output = read_server(cfg, env, server_argv)
        except (ValueError, OSError, subprocess.TimeoutExpired):
            state['server_provenance']['error'] = 'Could not read '+phase+' server provenance.'
            state.update(complete=False, exit_code=1)
            (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
            raise SystemExit(state['server_provenance']['error']) from None
        (run/('server-'+phase+'.log')).write_text(output)
        state['server_provenance'][phase] = snapshot
        return snapshot
    state['server'] = full_capture.profile_server(server_snapshot('before'))
    pass_history.preserve(state, run, HERE)
    def finish_provenance():
        server_snapshot('after')
        provenance = state['server_provenance']
        provenance['stable'] = provenance['before'] == provenance['after']
        state['inputs_stable'] = before == inputs()
        state['logs'] = {p.name:digest(p.read_bytes()) for p in run.iterdir()
                         if p.name.endswith('.log.gz') or p.name in ('server-before.log','server-after.log','prior-capture.json')}
    if state['hardware'] != ['Apple M4 Max','16','137438953472']:
        raise SystemExit('Hardware differs from this M4 Max / 16-core / 128 GiB reference; use a separate profile.')
    state['modules']=subprocess.check_output(['go','list','-m','all'],cwd=ROOT,env=env,text=True).splitlines()
    tracked=set(subprocess.check_output(['git','ls-files'],cwd=ROOT,text=True).splitlines())
    state['driver_matches_commit']=all(p in tracked and digest(subprocess.check_output(['git','show','HEAD:'+p],cwd=ROOT))==sha for p,sha in before['driver'].items())
    state['input_fingerprint']=digest(json.dumps(before,sort_keys=True).encode())
    state['identity_migration']='identity-migration.json; frozen bodies verified by inverse metadata substitutions'
    checkpoint(run, state, {'command': 'source preflight complete'})
    observed = {}
    check_outputs = {}
    commands = measurement_commands(run.resolve())
    for name,command_argv in FULL_COMMANDS.items():
        argv = list(command_argv)
        exit_code,output=command(name,argv)
        check_outputs[name] = output
        state['stages'][name]={'command':argv,'exit_code':exit_code,'cases':test_cases(output)}
        if name in ('upstream', 'protocol', 'mariadb'):
            observed[name] = test_cases(output, include_containers=True)
        print(' ',name,exit_code,dict(Counter(c.get('status','incomplete') for c in state['stages'][name]['cases'])),flush=True)
    state['check_issues'] = []
    actual_statuses = {case['name']:case.get('status') for cases in observed.values() for case in cases}
    state['lost_correctness_passes'] = pass_history.lost(state, run, actual_statuses)
    if state['lost_correctness_passes']:
        state['check_issues'].append('Prior correctness passing leaves lost: '+', '.join(state['lost_correctness_passes']))
    try:
        full_capture.validate_checks(state, check_outputs)
    except ValueError as error:
        state['check_issues'].append(str(error))
    if args.correctness_only or state['check_issues']:
        finish_provenance()
        state['lost_baseline_passes'] = lost_reference_passes([full_capture.prior_reference(state,run)],observed)
        state['correctness_only'] = args.correctness_only
        state['complete'] = state['inputs_stable'] and state['server_provenance']['stable'] and not state['check_issues'] and not state['lost_baseline_passes']
        state['exit_code'] = int(not state['complete'])
        (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
        print('Correctness evidence:', run, flush=True)
        return state['exit_code']
    for name,package in [('upstream','.'),('mariadb','./validation')]:
        argv=list(commands['build-'+name])
        code,_=command('build-'+name,argv)
        state['stages']['build-'+name]={'command':argv,'exit_code':code}
        if code:raise SystemExit('Benchmark build failed; refusing partial baseline.')
    # Seven fresh processes for each family. Fixed seeded shuffling alternates
    # family order without selecting favorable repetitions or rerunning timings.
    order=[(rep,family) for rep in range(7) for family in ['upstream','mariadb']]
    random.Random(20261111).shuffle(order)
    failed_bench=False
    for rep,family in order:
        state['host_observations'].append({'at':datetime.now(timezone.utc).isoformat(),'before_family':family,'repetition':rep,'top_cpu':subprocess.check_output(['ps','-Ao','pid,pcpu,comm','-r'],text=True).splitlines()[:9]})
        argv=list(commands[f'bench-{family}-{rep}'])
        code,output=command(f'bench-{family}-{rep}',argv)
        rows=benchmark_rows(output)
        state['stages'][f'bench-{family}-{rep}']={'command':argv,'exit_code':code,'profiles':len(rows)}
        failed_bench|=code!=0 or not rows
        for name,metrics in rows.items():
            state['benchmarks'].setdefault(name,{'samples':[]})['samples'].append({'repetition':rep,'family':family,**metrics})
    for name,row in state['benchmarks'].items():
        if len(row['samples'])!=7:failed_bench=True
        metrics=set.intersection(*(set(s) for s in row['samples']))-{'family','repetition'}
        row['median']={k:statistics.median(s[k] for s in row['samples']) for k in metrics}
    references = [json.loads((path/'capture.json').read_text()) for path in (baseline_dir,NATIVE_BASELINE,STAGE4_BASELINE) if path.exists()]
    state['benchmark_issues'] = benchmark_issues(state['benchmarks'], references) + stage4_issues(state['benchmarks'])
    finish_provenance()
    state['complete']=not failed_bench and not state['benchmark_issues'] and state['inputs_stable'] and state['server_provenance']['stable']
    state['exit_code']=int(not state['complete'] or any(s['exit_code'] for s in state['stages'].values()))
    (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
    if args.freeze_baseline:
        if not state['complete']:raise SystemExit('Incomplete evidence retained in run directory; baseline not frozen.')
        baseline_dir.mkdir(parents=True)
        shutil.copy2(run/'capture.json',baseline_dir/'capture.json')
        for log in run.glob('*.log.gz'):shutil.copy2(log,baseline_dir/log.name)
        (baseline_dir/'SHA256SUMS').write_text(''.join(digest(p.read_bytes())+'  '+p.name+'\n' for p in sorted(baseline_dir.iterdir()) if p.is_file()))
    baseline=json.loads((baseline_dir/'capture.json').read_text())
    native_reference = json.loads((NATIVE_BASELINE/'capture.json').read_text()) if NATIVE_BASELINE.exists() else None
    frozen_unchanged=baseline_workloads_match(baseline)
    modules_match=[case_key(m) for m in state['modules']]==baseline['modules']
    if not frozen_unchanged or not modules_match or baseline['server']!=state['server'] or baseline['go']!=state['go'] or baseline['host']!=state['host']:
        state.update(complete=False,exit_code=1,validation_issues=['Workload/environment differs from the reference profile.'])
        (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
        raise SystemExit('Workload/environment differs from baseline; retain a separate profile, do not publish a false comparison.')
    if native_reference and (native_reference['hardware'] != state['hardware'] or not native_workloads_match(native_reference, state['inputs'])):
        state.update(complete=False,exit_code=1,validation_issues=['Native workload/hardware differs from its first reference.'])
        (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
        raise SystemExit('Native workload differs from its first reference; retain a separate profile.')
    # Every original PASS remains protected independently of the aggregate count.
    state['lost_baseline_passes']=lost_reference_passes([baseline, full_capture.prior_reference(state,run)] + ([native_reference] if native_reference else []), observed)
    if state['lost_baseline_passes']:
        state.update(complete=False,exit_code=1)
    if state['complete'] and state['exit_code'] == 0:
        try:
            full_capture.validate(state, run)
        except (ValueError, OSError, KeyError) as error:
            state.update(complete=False,exit_code=1,validation_issues=[str(error)])
    if args.freeze_native_baseline:
        relevant = [c for st in ('upstream','mariadb') for c in state['stages'][st]['cases'] if '/TestDedicated' in c['name'] or '/TestMariaDBNative' in c['name']]
        if not state['complete'] or state['lost_baseline_passes'] or not relevant or any(c.get('status') != 'pass' for c in relevant):
            raise SystemExit('Native tests/comparisons incomplete; first baseline not frozen.')
        NATIVE_BASELINE.mkdir()
        native = {**state, 'native_workload_inputs':{'validation/native_test.go':state['inputs']['workload']['validation/native_test.go']}, 'benchmarks':{k:v for k,v in state['benchmarks'].items() if k.startswith('BenchmarkMariaDBNativeBulk/')}}
        (NATIVE_BASELINE/'capture.json').write_text(json.dumps(native,indent=2)+'\n')
        (NATIVE_BASELINE/'SHA256SUMS').write_text(digest((NATIVE_BASELINE/'capture.json').read_bytes())+'  capture.json\n')
    if args.freeze_stage4_baseline:
        if not state['complete'] or state['exit_code']:
            raise SystemExit('Incomplete Stage 4 evidence; reference not frozen.')
        STAGE4_BASELINE.mkdir()
        native = {**state, 'native_workload_inputs':{path:state['inputs']['workload'][path] for path in STAGE4_WORKLOAD_INPUTS},
                  'benchmarks':{name:state['benchmarks'][name] for name in STAGE4_PROFILES}}
        (STAGE4_BASELINE/'capture.json').write_text(json.dumps(native,indent=2)+'\n')
        (STAGE4_BASELINE/'SHA256SUMS').write_text(digest((STAGE4_BASELINE/'capture.json').read_bytes())+'  capture.json\n')
    (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
    # An incomplete attempt is useful evidence, but must not replace the latest
    # complete live report. Raw commands and missing identities stay in the run.
    if state['complete'] and state['exit_code'] == 0:
        # Publish the small, checksummed evidence, not ignored build binaries.
        # Verification must remain possible after the temporary runs are removed.
        evidence = HERE/state['evidence_directory']
        evidence.mkdir(parents=True)
        for name in state['logs']:
            shutil.copy2(run/name,evidence/name)
        shutil.copy2(run/pass_history.FILENAME,evidence/pass_history.FILENAME)
        shutil.copy2(run/'capture.json',evidence/'capture.json')
        (HERE/'results'/'current.json').write_text(json.dumps(state,indent=2)+'\n')
        render(state,baseline)
    print(f"Complete={state['complete']}; exit={state['exit_code']}; profiles={len(state['benchmarks'])}; evidence={run}")
    return state['exit_code']


if __name__=='__main__':
    sys.exit(main())
