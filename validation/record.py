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

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
PROFILE = 'macos-arm64-mariadb-12.3.3-go1.27.4'
NATIVE_BASELINE = HERE/'baselines'/('native-bulk-stage1-'+PROFILE)


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
    workload = {str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in files if p.name.endswith('_test.go')}
    workload.update({str(p.relative_to(ROOT)):digest(p.read_bytes()) for p in HERE.glob('*.py')})
    workload['validation/identity-migration.json'] = digest((HERE/'identity-migration.json').read_bytes())
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


def test_cases(text):
    """Count leaves once; retain parent failures separately in raw Go JSON."""
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
    parents = {key.rsplit('/', 1)[0] for key in records}
    return [value for key,value in sorted(records.items()) if key not in parents]


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


def render(current, baseline):
    features = json.loads((NATIVE_BASELINE/'capture.json').read_text())['benchmarks'] if NATIVE_BASELINE.exists() else {}
    lines = ['# Go MariaDB live validation and benchmarks', '',
             '> Dedicated MariaDB driver, Alpha. Baseline = unchanged upstream; Current = the measured working tree below.', '',
             f"Recorded: **{current['recorded_at']}** · Parent commit: `{current['commit'][:12]}` · Go: `{current['go']}`", '',
             f"Inputs: `{current.get('input_fingerprint','historical')}` · Matches committed source: **{current['driver_matches_commit']}**. See capture JSON for the complete dirty tree and per-file hashes.", '',
             f"Server: **{current['server']['version']}** · Host: `{current['host']}` · GOMAXPROCS: **4**.", '',
             f"Benchmark conditions: **{current['conditions']}**. Seven independent processes per benchmark family; medians and raw repetitions are retained.", '',
             *overview(current, baseline),
             '## Correctness', '', '| Suite | PASS | FAIL | SKIP | Exit |', '| --- | ---: | ---: | ---: | ---: |']
    for name,stage in current['stages'].items():
        counts=Counter(case.get('status','incomplete') for case in stage.get('cases',[]))
        lines.append(f"| {name} | {counts['pass']} | {counts['fail']} | {counts['skip']} | {stage['exit_code']} |")
    lines += ['', 'Leaf cases are counted once. Race cases repeat correctness coverage; do not add them to the ordinary total. Vet/build rows have no individual test cases.', '',
              '## Failed assertions', '',
              '| Suite | Baseline failures | Current failures |', '| --- | ---: | ---: |']
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
    lines += ['## Capability inventory', '', '| Capability | Baseline | Current | Evidence / next step |', '| --- | --- | --- | --- |']
    old_capabilities={item['name']:item['status'] for item in baseline['capabilities']}
    for item in current['capabilities']:
        lines.append(f"| {item['name']} | {old_capabilities.get(item['name'],'new inventory')} | {item['status']} | {item['evidence']} |")
    lines += ['', 'Inventory entries are source-reviewed API gaps, not passing feature tests. Missing external services are recorded separately from missing APIs. See [coverage and limits](COVERAGE.md).', '',
              '## Benchmark comparison', '',
              'Baseline = immutable unchanged-upstream capture for original workloads; [first native-bulk capture](baselines/native-bulk-stage1-macos-arm64-mariadb-12.3.3-go1.27.4/capture.json) for the new matched bulk profiles. Current = latest complete capture. Change = `(Current / Baseline - 1) × 100`; negative means less time. Units are **ns/operation**. The original added live reads consume 1,000 rows, BLOB reads consume 1 MiB, and original added writes replace 100 rows transactionally. Native bulk alternatives use **1/16/100/1000 rows per transaction**. Inherited workloads retain their own row counts and decoder/scanner units; do not apply the live-read row count to every inherited benchmark. Connection operations establish and close one connection.', '',
              '| Benchmark | Baseline ns/op | Current ns/op | Change vs baseline | Current B/op | Current allocs/op |',
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
    lines = ['## Current stage at a glance', '',
             f"🟢 **{counts['pass']} passing assertions** · 🔴 **{counts['fail']} failed assertions** · ⏭ **{counts['skip']} skips**.",
             'Ordinary totals exclude repeated race checks. The desired clean-session assertion remains an API gap; other failures are listed below.',
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
    parser.add_argument('--conditions',required=True,help='Observed/confirmed host conditions; never infer idle from silence')
    args=parser.parse_args()
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
    env=os.environ.copy()
    env.update(GOCACHE='/private/tmp/go-mariadb-validation-20261111/go-cache',GOMAXPROCS='4',
               MYSQL_TEST_USER=cfg['user'],MYSQL_TEST_PASS=cfg['password'],
               MYSQL_TEST_ADDR=f"{cfg['host']}:{cfg['port']}",MYSQL_TEST_DBNAME=cfg['upstream_database'],
               MARIADB_VALIDATION_CONFIG=str(args.config.resolve()))
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
    code,preflight=command('preflight',['mariadb','--protocol=socket','--batch','--skip-column-names','-e',
        "SELECT VERSION(),@@max_allowed_packet,@@local_infile,@@log_bin,@@performance_schema,@@innodb_flush_log_at_trx_commit,@@sync_binlog,@@sql_mode,@@time_zone,@@character_set_server,@@collation_server,@@max_connections"])
    if code or 'MariaDB' not in preflight:raise SystemExit('MariaDB preflight failed; no synthetic baseline permitted.')
    settings=preflight.strip().split('\t')
    before=inputs()
    state={'schema':1,'recorded_at':now.isoformat(),'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
           'go':subprocess.check_output(['go','version'],text=True).strip(),'host':platform.platform(),'conditions':args.conditions,
           'server':dict(zip(['version','max_allowed_packet','local_infile','log_bin','performance_schema','innodb_flush_log_at_trx_commit','sync_binlog','sql_mode','time_zone','charset','collation','max_connections'],settings)),
           'inputs':before,'stages':{},'benchmarks':{},'capabilities':json.loads((HERE/'capabilities.json').read_text()),
           'working_tree':subprocess.check_output(['git','status','--short'],cwd=ROOT,text=True),
           'host_observations':[],
           'hardware':subprocess.check_output(['sysctl','-n','machdep.cpu.brand_string','hw.ncpu','hw.memsize'],text=True).splitlines(),
           'method':{'process_repetitions':7,'benchtime':'200ms','gomaxprocs':4,'warmups':'original forty added serial profiles: two explicit warmups; native bulk and inherited/parallel profiles: Go benchmark calibration','profile':PROFILE}}
    if state['hardware'] != ['Apple M4 Max','16','137438953472']:
        raise SystemExit('Hardware differs from this M4 Max / 16-core / 128 GiB reference; use a separate profile.')
    state['modules']=subprocess.check_output(['go','list','-m','all'],cwd=ROOT,env=env,text=True).splitlines()
    tracked=set(subprocess.check_output(['git','ls-files'],cwd=ROOT,text=True).splitlines())
    state['driver_matches_commit']=all(p in tracked and digest(subprocess.check_output(['git','show','HEAD:'+p],cwd=ROOT))==sha for p,sha in before['driver'].items())
    state['input_fingerprint']=digest(json.dumps(before,sort_keys=True).encode())
    state['identity_migration']='identity-migration.json; frozen bodies verified by inverse metadata substitutions'
    checkpoint(run, state, {'command': 'source preflight complete'})
    for name,argv in [
        ('upstream',['go','test','-json','-count=1','-timeout=240s','.','./zstd','./internal/...']),
        ('protocol',['go','test','-json','-count=1','-timeout=60s','-tags=mariadb_validation','-run=^TestMariaDB','.']),
        ('mariadb',['go','test','-json','-count=1','-timeout=120s','./validation']),
        ('race',['go','test','-race','-json','-count=1','-timeout=240s','.','./zstd','./validation']),
        ('vet',['go','vet','./...']),
        ('recorder',['python3','-m','unittest','discover','-s','validation','-p','test_*.py']),
        ('fuzz',['go','test','-run=^$','-fuzz=^FuzzDedicatedLengthEncoding$','-fuzztime=8s','-parallel=4','.']),
        ('fuzz-terminator',['go','test','-run=^$','-fuzz=^FuzzDedicatedTerminator$','-fuzztime=8s','-parallel=4','.']),
    ]:
        exit_code,output=command(name,argv)
        state['stages'][name]={'command':argv,'exit_code':exit_code,'cases':test_cases(output)}
        print(' ',name,exit_code,dict(Counter(c.get('status','incomplete') for c in state['stages'][name]['cases'])),flush=True)
    if args.correctness_only:
        state['complete'] = before == inputs()
        state['exit_code'] = int(not state['complete'] or any(s['exit_code'] for s in state['stages'].values()))
        (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
        print('Correctness evidence:', run, flush=True)
        return state['exit_code']
    for name,package in [('upstream','.'),('mariadb','./validation')]:
        code,_=command('build-'+name,['go','test','-c','-o',str(run/(name+'.test')),package])
        state['stages']['build-'+name]={'exit_code':code}
        if code:raise SystemExit('Benchmark build failed; refusing partial baseline.')
    # Seven fresh processes for each family. Fixed seeded shuffling alternates
    # family order without selecting favorable repetitions or rerunning timings.
    order=[(rep,family) for rep in range(7) for family in ['upstream','mariadb']]
    random.Random(20261111).shuffle(order)
    failed_bench=False
    for rep,family in order:
        state['host_observations'].append({'at':datetime.now(timezone.utc).isoformat(),'before_family':family,'repetition':rep,'top_cpu':subprocess.check_output(['ps','-Ao','pid,pcpu,comm','-r'],text=True).splitlines()[:9]})
        code,output=command(f'bench-{family}-{rep}',[str(run/(family+'.test')),'-test.run=^$','-test.bench=.','-test.benchtime=200ms','-test.benchmem','-test.count=1','-test.timeout=240s'])
        rows=benchmark_rows(output)
        state['stages'][f'bench-{family}-{rep}']={'exit_code':code,'profiles':len(rows)}
        failed_bench|=code!=0 or not rows
        for name,metrics in rows.items():
            state['benchmarks'].setdefault(name,{'samples':[]})['samples'].append({'repetition':rep,'family':family,**metrics})
    for name,row in state['benchmarks'].items():
        if len(row['samples'])!=7:failed_bench=True
        metrics=set.intersection(*(set(s) for s in row['samples']))-{'family','repetition'}
        row['median']={k:statistics.median(s[k] for s in row['samples']) for k in metrics}
    references = [json.loads((path/'capture.json').read_text()) for path in (baseline_dir,NATIVE_BASELINE) if path.exists()]
    state['benchmark_issues'] = benchmark_issues(state['benchmarks'], references)
    state['inputs_stable']=before==inputs()
    state['complete']=not failed_bench and not state['benchmark_issues'] and state['inputs_stable']
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
        raise SystemExit('Workload/environment differs from baseline; retain a separate profile, do not publish a false comparison.')
    if native_reference and (native_reference['hardware'] != state['hardware'] or not native_workloads_match(native_reference, state['inputs'])):
        raise SystemExit('Native workload differs from its first reference; retain a separate profile.')
    # Every original PASS remains protected independently of the aggregate count.
    state['lost_baseline_passes']=[]
    for reference in [baseline] + ([native_reference] if native_reference else []):
        for name in ['upstream','protocol','mariadb']:
            statuses={case_key(case['name']):case.get('status') for case in state['stages'][name]['cases']}
            state['lost_baseline_passes'] += [case['name'] for case in reference['stages'][name]['cases'] if case.get('status')=='pass' and statuses.get(case_key(case['name']))!='pass']
    if state['lost_baseline_passes']:state['exit_code']=1
    if args.freeze_native_baseline:
        relevant = [c for st in ('upstream','mariadb') for c in state['stages'][st]['cases'] if '/TestDedicated' in c['name'] or '/TestMariaDBNative' in c['name']]
        if not state['complete'] or state['lost_baseline_passes'] or not relevant or any(c.get('status') != 'pass' for c in relevant):
            raise SystemExit('Native tests/comparisons incomplete; first baseline not frozen.')
        NATIVE_BASELINE.mkdir()
        native = {**state, 'native_workload_inputs':{'validation/native_test.go':state['inputs']['workload']['validation/native_test.go']}, 'benchmarks':{k:v for k,v in state['benchmarks'].items() if k.startswith('BenchmarkMariaDBNativeBulk/')}}
        (NATIVE_BASELINE/'capture.json').write_text(json.dumps(native,indent=2)+'\n')
        (NATIVE_BASELINE/'SHA256SUMS').write_text(digest((NATIVE_BASELINE/'capture.json').read_bytes())+'  capture.json\n')
    (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
    # An incomplete attempt is useful evidence, but must not replace the latest
    # complete live report. Raw commands and missing identities stay in the run.
    if state['complete']:
        (HERE/'results'/'current.json').write_text(json.dumps(state,indent=2)+'\n')
        render(state,baseline)
    print(f"Complete={state['complete']}; exit={state['exit_code']}; profiles={len(state['benchmarks'])}; evidence={run}")
    return state['exit_code']


if __name__=='__main__':
    sys.exit(main())
