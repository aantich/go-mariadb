#!/usr/bin/env python3
"""Prepare read diagnostics; execution is opt-in and never publishes timings.

Run without --execute to inspect commands while the host is busy. CPU/heap
profiles deliberately add overhead and are not accepted benchmark references.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

from contracts import FULL_CHECK_ENVIRONMENT
from provenance import read_server, server_command
import record

CASES = {
    'text-narrow-owned': ('narrow', 'false'),
    'text-blob-owned': ('blob', 'false'),
    'text-blob-borrowed': ('blob', 'true'),
}


def commands(case, output):
    shape, borrowed = CASES[case]
    binary = output/'validation.test'
    profile = 'BenchmarkMariaDBRead/text/'+shape+'/raw='+borrowed
    selector = '/'.join('^'+part+'$' for part in profile.split('/'))
    return profile, [
        ['go','test','-c','-o',str(binary),'./validation'],
        [str(binary),'-test.run=^$','-test.bench='+selector,'-test.benchtime=3s',
         '-test.count=1','-test.benchmem','-test.timeout=120s',
         '-test.cpuprofile='+str(output/'cpu.pprof'),
         '-test.memprofile='+str(output/'heap.pprof'),'-test.memprofilerate=1'],
    ]


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--case',choices=CASES,required=True)
    parser.add_argument('--config',type=Path,default=record.HERE/'.local/config.json')
    parser.add_argument('--output',type=Path,required=True,help='Use an ignored .local/profiles/<unique-run> directory; the executable is retained for go tool pprof.')
    parser.add_argument('--execute',action='store_true',help='Run diagnostics only after benchmark/profile execution is authorized.')
    parser.add_argument('--conditions',help='Observed host activity; required for execution, never infer idle.')
    args=parser.parse_args()
    output=args.output.resolve()
    profile, plan=commands(args.case,output)
    if not args.execute:
        print(json.dumps({'execute':False,'profile':profile,'commands':plan,
                          'conditions_required':True,'accepted_performance_measurement':False},indent=2))
        return 0
    if not args.conditions:
        raise SystemExit('--conditions is required for explicitly authorized diagnostics.')
    if output.exists():
        raise SystemExit('Retain earlier diagnostics; use a new output directory.')
    cfg=json.loads(args.config.read_text())
    if cfg['database']!='go_mariadb_validation' or cfg['upstream_database']!='go_mariadb_upstream':
        raise SystemExit('Refusing non-reserved fixture schemas.')
    client=shutil.which('mariadb') or shutil.which('mysql')
    if not client:
        raise SystemExit('MariaDB client is required for read-only endpoint provenance.')
    env={**os.environ,**FULL_CHECK_ENVIRONMENT,
         'MARIADB_VALIDATION_CONFIG':str(args.config.resolve())}
    env.pop('MYSQL_TEST_CONCURRENT',None)
    source=record.inputs()
    endpoint={key:cfg[key] for key in ('host','port','user','database')}
    argv=server_command(endpoint,client)
    output.mkdir(parents=True)
    capture={'recorded_at':datetime.now(timezone.utc).isoformat(),'case':args.case,'profile':profile,
             'conditions':args.conditions,'inputs':source,
             'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
             'check_environment':dict(FULL_CHECK_ENVIRONMENT),'stages':{},
             'accepted_performance_measurement':False,'server':{'endpoint':endpoint,'command':argv}}
    capture.update(status='in_progress', exit_code=1, inputs_stable=False)
    capture['server']['stable'] = False

    def redact(value):
        if isinstance(value, bytes):
            value = value.decode(errors='replace')
        value = value or ''
        return value.replace(cfg['password'], '[redacted]') if cfg.get('password') else value

    def save():
        # Retain an attributable failed attempt even if a probe/build is killed.
        # Atomic replacement keeps the last valid checkpoint readable.
        capture['artifacts'] = {name:record.digest((output/name).read_bytes()) for name in
            ('build.log','profile.log','server-before.log','server-after.log','cpu.pprof','heap.pprof','validation.test')
            if (output/name).is_file()}
        temporary = output/'capture.json.tmp'
        temporary.write_text(json.dumps(capture,indent=2)+'\n')
        temporary.replace(output/'capture.json')

    save()
    phase, stage = 'before', None
    try:
        before,text = read_server(cfg,env,argv)
        capture['server']['before'] = before
        (output/'server-before.log').write_text(redact(text))
        for name,command in zip(('build','profile'),plan):
            phase, stage = None, name
            capture['stages'][name] = {'command':command,'exit_code':None}
            save()
            result = subprocess.run(command,cwd=record.ROOT,env=env,text=True,capture_output=True,timeout=180)
            (output/(name+'.log')).write_text(redact(result.stdout)+redact(result.stderr))
            capture['stages'][name]['exit_code'] = result.returncode
            save()
            if result.returncode:
                break
        phase, stage = 'after', None
        after,text = read_server(cfg,env,argv)
        capture['server'].update(after=after,stable=before==after)
        (output/'server-after.log').write_text(redact(text))
        phase = 'validation'
        capture['inputs_stable'] = source==record.inputs()
        observed = record.benchmark_rows((output/'profile.log').read_text()) if (output/'profile.log').exists() else {}
        capture['exit_code'] = int(set(observed)!={profile} or not all((output/name).is_file() for name in ('cpu.pprof','heap.pprof'))
            or not capture['inputs_stable'] or not capture['server']['stable']
            or set(capture['stages'])!={'build','profile'} or any(s['exit_code']!=0 for s in capture['stages'].values()))
        capture['status'] = 'complete' if capture['exit_code']==0 else 'failed'
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        # Error messages/subprocess reprs can contain private input. Record the
        # failure class and location; redact any partial subprocess output.
        capture.update(status='failed',exit_code=1,error={'type':type(error).__name__,'phase':phase,'stage':stage})
        if stage is not None:
            capture['stages'][stage]['exit_code'] = 1
            if isinstance(error,subprocess.TimeoutExpired):
                (output/(stage+'.log')).write_text(redact(error.stdout)+redact(error.stderr))
        if phase in ('before','after'):
            capture['server']['error'] = 'Could not read '+phase+' server provenance.'
    finally:
        save()
    print('Diagnostic attempt retained; no baseline or live-result pointer updated: '+str(output))
    return capture['exit_code']


if __name__=='__main__':
    sys.exit(main())
