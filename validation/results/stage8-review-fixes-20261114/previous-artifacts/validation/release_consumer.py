#!/usr/bin/env python3
"""Build an independent offline consumer of a private source export.

The export contains only fingerprinted driver/module source and license notices.
It includes no credentials, repository metadata, validation results or workspaces.
This proves source packaging, not availability of an unpublished public version.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import record


def run(go):
    source=record.inputs()['driver']
    env={**os.environ,'GOWORK':'off','GOFLAGS':'','GOTOOLCHAIN':'local','GOPROXY':'off','CGO_ENABLED':'0'}
    with tempfile.TemporaryDirectory(prefix='go-mariadb-consumer-') as temporary:
        root=Path(temporary);export=root/'driver';consumer=root/'consumer';consumer.mkdir()
        for name,expected in source.items():
            destination=export/name;destination.parent.mkdir(parents=True,exist_ok=True)
            shutil.copyfile(record.ROOT/name,destination)
            if record.digest(destination.read_bytes())!=expected:raise ValueError('Driver export changed source.')
        shutil.copyfile(record.ROOT/'LICENSE',export/'LICENSE')
        (consumer/'go.mod').write_text('module mariadb-alpha-consumer\n\ngo 1.25.0\n\nrequire github.com/aantich/go-mariadb v0.0.0\n\nreplace github.com/aantich/go-mariadb => '+str(export)+'\n')
        shutil.copyfile(record.HERE/'apps/consumer/main.go',consumer/'main.go')
        # Dependency sums come from the unchanged core module. The consumer has
        # no dependency on the application frameworks or an active go.work file.
        shutil.copyfile(record.ROOT/'go.sum',consumer/'go.sum')
        commands=([go,'mod','tidy'],[go,'vet','./...'],[go,'build','-o',str(root/'consumer-bin'),'.'])
        for command in commands:
            result=subprocess.run(command,cwd=consumer,env=env,text=True,capture_output=True,timeout=120)
            if result.returncode:raise ValueError('Independent consumer command failed: '+command[1]+' '+result.stderr[-4000:])
        result=subprocess.run([str(root/'consumer-bin')],capture_output=True,text=True,check=True,timeout=10)
        if result.stdout!='MariaDB Alpha consumer ready\n':raise ValueError('Unexpected independent consumer output.')
        graph=subprocess.check_output([go,'list','-m','-json','all'],cwd=consumer,env=env,text=True,timeout=30)
        # No executable or private temporary path is retained in public output.
        print(json.dumps({'contract':'mariadb-consumer-v1','driver_files':source,
            'core_go_mod_sha256':record.digest((record.ROOT/'go.mod').read_bytes()),
            'consumer_source_sha256':record.digest((consumer/'main.go').read_bytes()),
            'cgo_enabled':False,'workspace':'off','network':'off','build':True,'vet':True,
            'output':result.stdout.strip(),'framework_dependencies_present':any(name in graph for name in ('gorm.io','jmoiron/sqlx')),
            'temporary_export_removed_on_exit':True}))


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--go',required=True)
    run(parser.parse_args().go)
