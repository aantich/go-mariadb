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

CONTRACT = 'mariadb-applications-native-v1'


def socket_command(endpoint, program):
    return [program, '--no-defaults', '--protocol=socket', '--socket='+endpoint['socket'],
            '--user=root', '--database=go_mariadb_validation', '--batch', '--raw',
            '--skip-column-names', '--execute='+provenance.SERVER_QUERY]


def validate(capture, directory, *, current=True):
    apps.validate_checks(capture, directory, current)
    compiler=capture['compiler']['version']
    if (capture.get('native_contract') != CONTRACT or capture.get('resources_cleaned') is not True
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
    return capture


def child(go, directory):
    control=Path(os.environ['GO_MARIADB_CDC_CONTROL']).resolve()
    socket=Path(os.environ['GO_MARIADB_CDC_SOCKET']).resolve()
    if socket.parent != control:
        raise ValueError('Application socket is outside the owned fixture.')
    directory.mkdir(parents=True,exist_ok=False)
    program=shutil.which('mariadb')
    subprocess.run([program,'--no-defaults','--protocol=socket','--socket='+str(socket),'--user=root'],
                   input='CREATE DATABASE go_mariadb_validation CHARACTER SET utf8mb4',text=True,check=True,timeout=10)
    config=control/'application-config.json'
    config.write_text(json.dumps({'socket':str(socket),'user':'root','password':'',
        'database':'go_mariadb_validation','tls_ca':os.environ['GO_MARIADB_CDC_TLS_CA']}))
    config.chmod(0o600)
    env={**os.environ,**compatibility.ENVIRONMENT,'MARIADB_VALIDATION_CONFIG':str(config)}
    env.pop('GO_MARIADB_MATRIX_CONTROL',None)
    binary=str(Path(shutil.which(go) or go).resolve())
    source=record.inputs()
    capture={'contract':apps.CONTRACT,'native_contract':CONTRACT,'method':apps.METHOD,
        'native_fixture':json.loads(os.environ['GO_MARIADB_CDC_RECEIPT']),
        'recorded_at':datetime.now(timezone.utc).isoformat(),**provenance.revision_info(record.ROOT),
        'host':platform.platform(),'source_directory':str(record.ROOT),
        'compiler':{'path':binary,'sha256':record.digest(Path(binary).read_bytes()),
            'version':subprocess.check_output([binary,'version'],env=env,text=True,timeout=10).strip()},
        'inputs':source,'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
        'environment':compatibility.ENVIRONMENT,'required_tests':apps.required(),
        'stages':{},'logs':{},'exit_code':1,'inputs_stable':False,'resources_cleaned':False,'restart_count':0}
    endpoint={'socket':str(socket)};command=socket_command(endpoint,program)
    def observe(phase):
        raw=subprocess.check_output(command,env=env,text=True,timeout=10)
        (directory/f'server-{phase}.log').write_text(raw)
        return provenance.parse_server(raw)
    capture['server']={'endpoint':endpoint,'command':command,'before':observe('before')}
    try:
        for name,argv in apps.commands(binary).items():
            # The fixture parent services restart requests while these bounded
            # children run. It also owns and reaps the entire process group.
            result=subprocess.run(argv,cwd=record.ROOT,env=env,text=True,
                                  stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=300)
            (directory/(name+'.log')).write_text(result.stdout)
            issues,cases=apps.assess(result.stdout,result.returncode,name)
            capture['stages'][name]={'command':argv,'exit_code':result.returncode,'issues':issues,'cases':cases}
            print(f'Native applications {name}: exit={result.returncode}, issues={len(issues)}',flush=True)
            if result.returncode or issues:raise ValueError('Native application check failed: '+name)
        capture['server']['after']=observe('after')
        capture['server']['stable']=capture['server']['before']==capture['server']['after']
        capture['inputs_stable']=source==record.inputs()
        capture['restart_count']=1
        capture['exit_code']=0
    finally:
        shutil.copyfile(control/'server.log',directory/'server.log')
        capture['logs']={path.name:record.digest(path.read_bytes()) for path in directory.glob('*.log')}
        (directory/'capture.json').write_text(json.dumps(capture,indent=2)+'\n')
    return 0


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
    validate(capture,directory)
    return 0


if __name__=='__main__':raise SystemExit(main())
