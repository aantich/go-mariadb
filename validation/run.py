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

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
PROFILE = 'macos-arm64-mariadb-12.3.3-go1.27.4'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inputs():
    tracked = subprocess.check_output(['git', 'ls-files'], cwd=ROOT, text=True).splitlines()
    driver = {p: digest((ROOT/p).read_bytes()) for p in tracked
              if (p.endswith('.go') and not p.endswith('_test.go')) or p in ['go.mod', 'go.sum']}
    workload = {p: digest((ROOT/p).read_bytes()) for p in tracked if p.endswith('_test.go')}
    paths = [ROOT/'mariadb_validation_protocol_test.go', *HERE.glob('*.go'), *HERE.glob('*.py')]
    workload.update({str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in paths if p.exists()})
    return {'driver': driver, 'workload': workload}


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
    lines = ['# Go MariaDB live validation and benchmarks', '',
             '> Initial baseline: the inherited driver is unchanged. Failures and gaps are deliberately retained.', '',
             f"Recorded: **{current['recorded_at']}** · Driver commit: `{current['commit'][:12]}` · Go: `{current['go']}`", '',
             f"Server: **{current['server']['version']}** · Host: `{current['host']}` · GOMAXPROCS: **4**.", '',
             f"Benchmark conditions: **{current['conditions']}**. Seven independent processes per benchmark family; medians and raw repetitions are retained.", '',
             '## Correctness', '', '| Suite | PASS | FAIL | SKIP | Exit |', '| --- | ---: | ---: | ---: | ---: |']
    for name,stage in current['stages'].items():
        counts=Counter(case.get('status','incomplete') for case in stage.get('cases',[]))
        lines.append(f"| {name} | {counts['pass']} | {counts['fail']} | {counts['skip']} | {stage['exit_code']} |")
    lines += ['', 'Leaf cases are counted once. Race cases repeat correctness coverage; do not add them to the ordinary total. Vet/build rows have no individual test cases.', '',
              '## Failed assertions', '']
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
              'Baseline = immutable first capture of this workload and environment. Current = latest complete capture. Change = `(Current / Baseline - 1) × 100`; negative means less time. The unit below is **ns/operation**. Read operations consume 1,000 rows, BLOB operations consume 1 MiB, writes replace 100 rows transactionally, and connection operations establish and close one connection.', '',
              '| Benchmark | Baseline ns/op | Current ns/op | Change vs baseline | Current B/op | Current allocs/op |',
              '| --- | ---: | ---: | ---: | ---: | ---: |']
    for name,row in sorted(current['benchmarks'].items()):
        old=baseline['benchmarks'].get(name)
        before=old['median']['ns/op'] if old else None
        now=row['median']['ns/op']
        change=f'{(now/before-1)*100:+.1f}%' if before else 'new workload'
        lines.append(f"| `{name}` | {before:,.1f} | {now:,.1f} | {change} | {row['median'].get('B/op',0):,.0f} | {row['median'].get('allocs/op',0):,.0f} |" if before else f'| `{name}` | — | {now:,.1f} | new workload | — | — |')
    lines += ['', 'These are Go-only measurements, not a speed comparison with the Haskell driver or Connector/C. Metadata controls use identical SQL, scanner and network-accounting wrappers; only the advertised metadata-cache capability changes.', '',
              '## Reproduce and preserve history', '',
              'See [validation instructions](README.md), [review findings](../docs/MARIADB_REVIEW.md) and [implementation roadmap](../docs/ROADMAP.md).', '',
              'Raw logs, hashes, process repetitions and server settings are in the capture JSON and compressed baseline logs. A nonzero full-run exit is retained when correctness fails; benchmark completion does not turn it into a pass.', '']
    (HERE/'RESULTS.md').write_text('\n'.join(lines))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config',type=Path,default=HERE/'.local/config.json')
    parser.add_argument('--freeze-baseline',action='store_true')
    parser.add_argument('--conditions',required=True,help='Observed/confirmed host conditions; never infer idle from silence')
    args=parser.parse_args()
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
    def command(name,argv):
        print(name,flush=True)
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
           'method':{'process_repetitions':7,'benchtime':'200ms','gomaxprocs':4,'warmups':'new serial workloads: two explicit warmups; inherited/parallel workloads: Go benchmark calibration','profile':PROFILE}}
    state['modules']=subprocess.check_output(['go','list','-m','all'],cwd=ROOT,env=env,text=True).splitlines()
    state['driver_matches_commit']=all(digest(subprocess.check_output(['git','show','HEAD:'+p],cwd=ROOT))==sha for p,sha in before['driver'].items())
    for name,argv in [
        ('upstream',['go','test','-json','-count=1','-timeout=240s','.','./zstd','./internal/...']),
        ('protocol',['go','test','-json','-count=1','-timeout=60s','-tags=mariadb_validation','-run=^TestMariaDB','.']),
        ('mariadb',['go','test','-json','-count=1','-timeout=120s','./validation']),
        ('race',['go','test','-race','-json','-count=1','-timeout=240s','.','./zstd','./validation']),
        ('vet',['go','vet','./...']),
    ]:
        exit_code,output=command(name,argv)
        state['stages'][name]={'command':argv,'exit_code':exit_code,'cases':test_cases(output)}
        print(' ',name,exit_code,dict(Counter(c.get('status','incomplete') for c in state['stages'][name]['cases'])),flush=True)
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
    state['inputs_stable']=before==inputs()
    state['complete']=not failed_bench and state['inputs_stable']
    state['exit_code']=int(not state['complete'] or any(s['exit_code'] for s in state['stages'].values()))
    (run/'capture.json').write_text(json.dumps(state,indent=2)+'\n')
    if args.freeze_baseline:
        if not state['complete']:raise SystemExit('Incomplete evidence retained in run directory; baseline not frozen.')
        baseline_dir.mkdir(parents=True)
        shutil.copy2(run/'capture.json',baseline_dir/'capture.json')
        for log in run.glob('*.log.gz'):shutil.copy2(log,baseline_dir/log.name)
        (baseline_dir/'SHA256SUMS').write_text(''.join(digest(p.read_bytes())+'  '+p.name+'\n' for p in sorted(baseline_dir.iterdir()) if p.is_file()))
    baseline=json.loads((baseline_dir/'capture.json').read_text())
    frozen_unchanged=all(state['inputs']['workload'].get(p)==sha for p,sha in baseline['inputs']['workload'].items())
    if not frozen_unchanged or baseline['server']!=state['server'] or baseline['go']!=state['go'] or baseline['host']!=state['host']:
        raise SystemExit('Workload/environment differs from baseline; retain a separate profile, do not publish a false comparison.')
    # Every original PASS remains protected independently of the aggregate count.
    state['lost_baseline_passes']=[]
    for name in ['upstream','protocol','mariadb']:
        statuses={case['name']:case.get('status') for case in state['stages'][name]['cases']}
        state['lost_baseline_passes'] += [case['name'] for case in baseline['stages'][name]['cases'] if case.get('status')=='pass' and statuses.get(case['name'])!='pass']
    if state['lost_baseline_passes']:state['exit_code']=1
    (HERE/'results'/'current.json').write_text(json.dumps(state,indent=2)+'\n')
    render(state,baseline)
    print(f"Complete={state['complete']}; exit={state['exit_code']}; profiles={len(state['benchmarks'])}; evidence={run}")
    return state['exit_code']


if __name__=='__main__':
    sys.exit(main())
