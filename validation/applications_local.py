#!/usr/bin/env python3
"""Supplement the version matrix with applications on a native Linux runtime.

The parent owns the private socket server through cdc_fixture.py. The child
cannot point this command at an arbitrary running database. Cleanup is asserted
only after that parent has returned successfully, including its finally block.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys

import applications as apps
import compatibility
import provenance
import record

CONTRACT = 'mariadb-applications-native-v2'
HISTORICAL_CONTRACTS = {'mariadb-applications-native-v1', CONTRACT}


def socket_command(endpoint, program):
    return [program, '--no-defaults', '--protocol=socket', '--socket='+endpoint['socket'],
            '--user=root', '--database=go_mariadb_validation', '--batch', '--raw',
            '--skip-column-names', '--execute='+provenance.SERVER_QUERY]


def validate(capture, directory, *, current=True):
    directory = Path(directory)
    apps.validate_checks(capture, directory, current)
    compiler=capture['compiler']['version']
    if (capture.get('native_contract') not in ({CONTRACT} if current else HISTORICAL_CONTRACTS) or capture.get('resources_cleaned') is not True
            or type(capture.get('restart_count')) is not int or capture['restart_count'] != 1
            or not compiler.startswith('go version go1.27.')
            or not compiler.endswith(('linux/arm64','linux/amd64'))
            or not capture['server']['before']['version'].startswith('12.3.')):
        raise ValueError('Incomplete native Linux application profile.')
    receipt=capture.get('native_fixture', {})
    expected=capture['inputs']['workload']['validation/cdc_fixture.py']
    if (receipt.get('contract') != 'cdc-v1' or receipt.get('fixture_sha256') != expected
            or receipt.get('transport') != 'private-unix-socket'
            or receipt.get('server_version') != capture['server']['before']['version']):
        raise ValueError('Wrong native application fixture.')
    server=capture['server'];command=server['command']
    if (set(server['endpoint']) != {'socket'} or not Path(server['endpoint']['socket']).is_absolute()
            or Path(command[0]).name != 'mariadb' or command != socket_command(server['endpoint'],command[0])):
        raise ValueError('Wrong private application socket provenance.')
    for phase in ('before','after'):
        if provenance.parse_server((directory/f'server-{phase}.log').read_text()) != server[phase]:
            raise ValueError('Native application server provenance differs.')
    if server['before'] != server['after'] or server.get('stable') is not True:
        raise ValueError('Native application server settings changed.')
    # V1 receipts predate native pass-history protection. They remain immutable
    # historical predecessors, but cannot be published as current evidence.
    if capture['native_contract'] == CONTRACT:
        previous = capture.get('prior_profile')
        if previous:
            path = (record.HERE / previous['source']).resolve()
            if (not path.is_relative_to((record.HERE/'results').resolve())
                    or record.digest(path.read_bytes()) != previous['sha256']):
                raise ValueError('Native application history source changed.')
            old = json.loads(path.read_text())
            if old['recorded_at'] >= capture['recorded_at']:
                raise ValueError('Native application predecessor is not earlier.')
            validate(old, path.parent, current=False)
            if (profile(old) != profile(capture) or previous.get('profile') != profile(old)
                    or previous['passing'] != sorted(apps.passing(old))):
                raise ValueError('Native application history snapshot differs.')
            if set(previous['passing']) - apps.passing(capture):
                raise ValueError('Earlier native application passes lost.')
        if capture.get('lost_prior_passes') != []:
            raise ValueError('Earlier native application passes lost.')
        if current and previous != prior_profile(capture):
            raise ValueError('Newest native application predecessor was omitted.')
    return capture


def profile(capture):
    """Match runtime/configuration, independent of source growth or output name.

    Random private socket paths and the fixture script hash are provenance, not
    runtime identity. Including them would silently reset history on every run
    or fixture edit. Server settings and meaningful fixture options stay fixed.
    """
    fixture = capture['native_fixture']
    return {'compiler_sha256':capture['compiler']['sha256'],
            'go':capture['compiler']['version'], 'host':capture['host'],
            'server':capture['server']['before'], 'environment':capture['environment'],
            'fixture':{key:value for key,value in fixture.items() if key != 'fixture_sha256'}}


def prior_profile(capture):
    """Find the newest eligible native predecessor and verify it fail closed."""
    eligible = []
    results = (record.HERE/'results').resolve()
    for path in results.rglob('capture.json'):
        if not path.resolve().is_relative_to(results):
            continue
        try:
            old = json.loads(path.read_text())
        except (ValueError, OSError):
            continue
        if (old.get('contract') != apps.CONTRACT or not old.get('native_contract')
                or old.get('exit_code') != 0 or old.get('recorded_at','') >= capture['recorded_at']):
            continue
        if profile(old) != profile(capture):
            continue
        validate(old, path.parent, current=False)
        eligible.append((old['recorded_at'], str(path), old))
    if not eligible:
        return None
    _, name, old = max(eligible, key=lambda entry:entry[:2])
    path = Path(name)
    return {'profile':profile(old), 'source':str(path.relative_to(record.HERE)),
            'sha256':record.digest(path.read_bytes()), 'passing':sorted(apps.passing(old))}


def run_stage(name, argv, env, directory):
    """Keep output on disk even if a bounded child times out or is interrupted.

    cdc_fixture owns this process group and reaps descendants after the child
    exits. Do not put a timeout around that fixture owner in the parent.
    """
    path = directory/(name+'.log')
    with path.open('w') as output:
        try:
            result = subprocess.run(argv, cwd=record.ROOT, env=env, text=True,
                                    stdout=output, stderr=subprocess.STDOUT, timeout=300)
        except subprocess.TimeoutExpired as error:
            # Real streamed children leave output in the file. Also retain
            # partial bytes supplied by transports/mocks that buffer output.
            if output.tell() == 0 and error.output:
                partial = error.output
                output.write(partial.decode('utf-8', errors='replace') if isinstance(partial, bytes) else partial)
            return {'command':argv, 'exit_code':None, 'timed_out':True,
                    'issues':['Native application stage timed out.'], 'cases':{}}
    raw = path.read_text(errors='replace')
    issues, cases = apps.assess(raw, result.returncode, name)
    return {'command':argv, 'exit_code':result.returncode, 'issues':issues, 'cases':cases}


def child(go, directory):
    directory.mkdir(parents=True, exist_ok=False)
    capture = {'contract':apps.CONTRACT, 'native_contract':CONTRACT, 'method':apps.METHOD,
        'recorded_at':datetime.now(timezone.utc).isoformat(), 'host':platform.platform(),
        'source_directory':str(record.ROOT), 'environment':compatibility.ENVIRONMENT,
        'required_tests':apps.required(), 'stages':{}, 'logs':{}, 'exit_code':1,
        'inputs_stable':False, 'resources_cleaned':False, 'restart_count':0,
        'prior_profile':None, 'lost_prior_passes':[]}
    control = None
    phase = 'fixture_preflight'
    try:
        control = Path(os.environ['GO_MARIADB_CDC_CONTROL']).resolve()
        socket = Path(os.environ['GO_MARIADB_CDC_SOCKET']).resolve()
        if socket.parent != control:
            raise ValueError('Application socket is outside the owned fixture.')
        capture['native_fixture'] = json.loads(os.environ['GO_MARIADB_CDC_RECEIPT'])
        capture.update(provenance.revision_info(record.ROOT))
        source = record.inputs()
        capture.update(inputs=source, input_fingerprint=record.digest(json.dumps(source,sort_keys=True).encode()))
        program = shutil.which('mariadb')
        phase = 'fixture_schema'
        subprocess.run([program,'--no-defaults','--protocol=socket','--socket='+str(socket),'--user=root'],
            input='CREATE DATABASE go_mariadb_validation CHARACTER SET utf8mb4', text=True,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True, timeout=10)
        config = control/'application-config.json'
        config.write_text(json.dumps({'socket':str(socket), 'user':'root', 'password':'',
            'database':'go_mariadb_validation', 'tls_ca':os.environ['GO_MARIADB_CDC_TLS_CA']}))
        config.chmod(0o600)
        env = {**os.environ, **compatibility.ENVIRONMENT, 'MARIADB_VALIDATION_CONFIG':str(config)}
        env.pop('GO_MARIADB_MATRIX_CONTROL',None)
        phase = 'compiler_identity'
        binary = str(Path(shutil.which(go) or go).resolve())
        capture['compiler'] = {'path':binary, 'sha256':record.digest(Path(binary).read_bytes()),
            'version':subprocess.check_output([binary,'version'],env=env,text=True,timeout=10).strip()}
        endpoint = {'socket':str(socket)}
        command = socket_command(endpoint, program)
        def observe(when):
            raw = subprocess.check_output(command,env=env,text=True,timeout=10)
            (directory/f'server-{when}.log').write_text(raw)
            return provenance.parse_server(raw)
        phase = 'server_provenance_before'
        capture['server'] = {'endpoint':endpoint, 'command':command, 'before':observe('before')}
        phase = 'pass_history'
        capture['prior_profile'] = prior_profile(capture)
        for name,argv in apps.commands(binary).items():
            phase = 'check_'+name
            stage = run_stage(name, argv, env, directory)
            capture['stages'][name] = stage
            print(f'Native applications {name}: exit={stage["exit_code"]}, issues={len(stage["issues"])}',flush=True)
            if stage['exit_code'] != 0 or stage['issues']:
                raise RuntimeError('Native application stage failed.')
        phase = 'server_provenance_after'
        capture['server']['after'] = observe('after')
        capture['server']['stable'] = capture['server']['before'] == capture['server']['after']
        capture['inputs_stable'] = source == record.inputs()
        previous = capture['prior_profile']
        capture['lost_prior_passes'] = sorted(set(previous['passing'])-apps.passing(capture)) if previous else []
        capture['restart_count'] = 1
        if capture['lost_prior_passes'] or not capture['inputs_stable'] or not capture['server']['stable']:
            raise ValueError('Native application history, source or server changed.')
        capture['exit_code'] = 0
    except (Exception, KeyboardInterrupt) as error:
        # Never serialize arbitrary exception text, SQL or subprocess environments.
        # Test-stage output is already on disk; setup failures retain safe phase/type.
        compatibility.save_failure(capture, directory, error, phase)
        print('Native applications failed; inspect '+str(directory/'capture.json')+'.',flush=True)
    finally:
        try:
            if control is not None:
                shutil.copyfile(control/'server.log',directory/'server.log')
        except OSError as error:
            capture['exit_code'] = 1
            compatibility.save_failure(capture,directory,error,'server_diagnostics','server_log_failure')
        try:
            capture['logs'] = {path.name:record.digest(path.read_bytes()) for path in directory.glob('*.log')}
            (directory/'capture.json').write_text(json.dumps(capture,indent=2)+'\n')
        except OSError as error:
            # Returning still lets the fixture parent reap the owned process
            # group/server even when evidence storage itself has failed.
            print('Native application receipt write failed: '+type(error).__name__,file=sys.stderr)
            capture['exit_code'] = 1
    return capture['exit_code']


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go',default='go');parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--child',action='store_true',help=argparse.SUPPRESS)
    args=parser.parse_args();directory=args.output.resolve()
    if not directory.is_relative_to((record.HERE/'results').resolve()) or directory.exists():
        raise ValueError('Retain prior attempts in a new validation/results directory.')
    if args.child:return child(args.go,directory)
    command=[sys.executable,str(record.HERE/'cdc_fixture.py'),'--',sys.executable,
             str(Path(__file__).resolve()),'--child','--go',args.go,'--output',str(directory)]
    # cdc_fixture bounds initialization, readiness and child execution itself.
    # Killing that owner with an outer timeout would bypass its finally cleanup.
    result=subprocess.run(command,cwd=record.ROOT)
    if result.returncode:return result.returncode
    path=directory/'capture.json';capture=json.loads(path.read_text())
    capture['resources_cleaned']=True
    path.write_text(json.dumps(capture,indent=2)+'\n')
    try:
        validate(capture,directory)
    except (ValueError,KeyError,OSError) as error:
        capture['exit_code']=1
        compatibility.save_failure(capture,directory,error,'receipt_validation')
        capture['logs']={path.name:record.digest(path.read_bytes()) for path in directory.glob('*.log')}
        path.write_text(json.dumps(capture,indent=2)+'\n')
        return 1
    return 0


if __name__=='__main__':raise SystemExit(main())
