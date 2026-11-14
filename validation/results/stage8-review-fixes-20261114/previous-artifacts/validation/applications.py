#!/usr/bin/env python3
"""Correctness-only application profiles; no benchmark or publication side effects."""
import argparse
from collections import Counter
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys

import ci
import compatibility
import correctness
from profile_suite import ProfileSuite
import provenance
import record

CONTRACT='mariadb-applications-v1'
METHOD='application correctness only'
CONTRACT_SHA256 = 'a7f22967a68487972e264327e6980e89df5c51946ce22c847827c2921550b49e'
PACKAGE=ci.PACKAGE+'/validation/apps'
ADAPTER=ci.PACKAGE+'/integrations/gorm'
REQUIRED={'applications':{PACKAGE+'/'+name for name in (
    'TestApplicationProvenance','TestGORMApplication','TestSQLxApplication',
    'TestApplicationLifecycle','TestApplicationManagedSessions','TestApplicationRestart','TestApplicationExamples')},
    'adapter':{ADAPTER+'/TestTranslationIdentity',ADAPTER+'/TestDedicatedPoolRequired'}}
for name in ('TestGORMApplication','TestSQLxApplication'):
    REQUIRED['applications'].update(PACKAGE+'/'+name+'/'+mode for mode in ('plain','compressed','tls','tls_compressed'))
for client in ('sql','sqlx','gorm'):
    REQUIRED['applications'].update(PACKAGE+'/TestApplicationLifecycle/'+client+'/'+name for name in (
        'pool_wait_cancellation','active_query_cancellation','early_rows_close','prepared_handle_expiry','concurrent_requests'))
REQUIRED['adapter'].update(ADAPTER+'/TestTranslationIdentity/'+number for number in ('1062','1451','1452'))


def commands(go):
    test=['test','-race','-json','-count=1','-timeout=180s','-p=1','-parallel=2','./...']
    return {'applications':[go,'-C','validation/apps',*test],
            'adapter':[go,'-C','integrations/gorm',*test],
            'vet':[go,'-C','validation/apps','vet','./...'],
            'adapter-vet':[go,'-C','integrations/gorm','vet','./...'],
            'modules':[go,'-C','validation/apps','list','-m','-json','all'],
            'consumer':['python3','validation/release_consumer.py','--go',go]}


def required():return {name:sorted(values) for name,values in REQUIRED.items()}


def frozen_contract():
    """Historical passes use their original command, inventory and lock graph."""
    data=(record.HERE/'applications-contract-v1.json').read_bytes()
    if record.digest(data)!=CONTRACT_SHA256:
        raise ValueError('The immutable application contract changed.')
    return json.loads(data)


def assess(output,code,name):
    if name in REQUIRED:
        return ci.assess_tests(output,code,required=REQUIRED[name])
    return (['Command failed: '+name] if code else []),{}


def passing(capture):
    return {stage+'/'+name for stage in REQUIRED
            for name,value in correctness.leaves(capture['stages'][stage]['cases']).items()
            if value.get('status')=='pass'}


def events(output,test,marker):
    result=[]
    for line in output.splitlines():
        try:event=json.loads(line)
        except ValueError:continue
        if marker in event.get('Output',''):
            if event.get('Action')!='output' or event.get('Package')!=PACKAGE or event.get('Test')!=test:
                raise ValueError('Misattributed application marker.')
            result.append(event['Output'].split(marker,1)[1].strip())
    if len(result)!=1:raise ValueError('Expected exactly one application marker: '+marker)
    return result[0]


def module_graph(output,source_directory):
    """Normalize location-independent lock data and verify local replacements."""
    root=Path(source_directory)
    if not root.is_absolute():raise ValueError('Missing application source directory.')
    decoder=json.JSONDecoder();result=[]
    while output.strip():
        value,n=decoder.raw_decode(output.lstrip());output=output.lstrip()[n:]
        item={key:value[key] for key in ('Path','Version','Sum','GoModSum','GoVersion','Main') if key in value}
        if 'Replace' in value:
            suffix={'github.com/aantich/go-mariadb':'',ADAPTER:'integrations/gorm'}.get(value['Path'])
            if suffix is None or Path(value['Replace'].get('Dir','')).resolve()!=(root/suffix).resolve():
                raise ValueError('Application dependency resolved outside the checked source.')
            item['local_source']=value['Path']
        result.append(item)
    if len({item['Path'] for item in result})!=len(result):raise ValueError('Duplicate dependency graph entry.')
    return sorted(result,key=lambda item:item['Path'])


def validate_checks(capture,directory,current=True):
    directory=Path(directory)
    if capture.get('contract')!=CONTRACT or capture.get('method')!=METHOD or capture.get('exit_code')!=0 or capture.get('inputs_stable') is not True:
        raise ValueError('Failed or incomplete application receipt.')
    if current and capture.get('inputs')!=record.inputs():raise ValueError('Stale application source.')
    if capture.get('input_fingerprint')!=record.digest(json.dumps(capture['inputs'],sort_keys=True).encode()):raise ValueError('Wrong application fingerprint.')
    if capture.get('environment')!=compatibility.ENVIRONMENT:raise ValueError('Wrong application environment.')
    compiler=capture.get('compiler',{})
    if not Path(compiler.get('path','')).is_absolute() or not re.fullmatch('[0-9a-f]{64}',compiler.get('sha256','')) or not re.fullmatch(r'go version go1\.(25|27)\.\d+ (darwin|linux)/(arm64|amd64)',compiler.get('version','')):
        raise ValueError('Unverified application compiler.')
    frozen=frozen_contract()
    expected=commands(compiler['path']) if current else {name:[compiler['path'] if arg=='$GO' else arg for arg in args] for name,args in frozen['commands'].items()}
    inventory=capture.get('required_tests',{})
    if current and inventory!=required():raise ValueError('Changed application inventory.')
    for stage,names in frozen['required_tests'].items():
        if not isinstance(inventory.get(stage),list) or not set(names).issubset(inventory[stage]):raise ValueError('Incomplete historical application inventory.')
    if set(capture.get('stages',{}))!=set(expected):raise ValueError('Missing application stages.')
    logs={name+'.log' for name in expected}|{'server-before.log','server-after.log','server.log'}
    if set(capture.get('logs',{}))!=logs:raise ValueError('Incomplete application log set.')
    results=(record.HERE/'results').resolve()
    for name,digest in capture['logs'].items():
        path=(directory/name).resolve()
        if not path.is_relative_to(results) or Path(name).name!=name or record.digest(path.read_bytes())!=digest:raise ValueError('Application log path/hash mismatch.')
    for name,command in expected.items():
        stage=capture['stages'][name]
        if stage.get('command')!=command or stage.get('exit_code')!=0 or stage.get('issues')!=[]:raise ValueError('Wrong application command: '+name)
        if name in REQUIRED:
            issues,cases=ci.assess_tests((directory/(name+'.log')).read_text(),0,required=set(inventory[name]))
            if issues or cases!=stage.get('cases'):raise ValueError('Application raw cases differ or required work is absent.')
        elif stage.get('cases')!={}:raise ValueError('Unexpected non-test cases.')
    raw=(directory/'applications.log').read_text()
    marker=json.loads(events(raw,'TestApplicationProvenance','MARIADB_APPLICATION '))
    if marker!={'server_version':capture['server']['before']['version'],'driver_module':ci.PACKAGE,'registration':'mariadb'}:raise ValueError('Application driver/server identity differs.')
    if events(raw,'TestApplicationRestart','APPLICATION_RESTART ')!='completed=1':raise ValueError('Missing application restart proof.')
    graph=json.loads((record.HERE/'apps/dependencies.json').read_text()) if current else frozen['dependencies']
    if module_graph((directory/'modules.log').read_text(),capture['source_directory'])!=graph:raise ValueError('Application module graph changed.')
    consumer=json.loads((directory/'consumer.log').read_text())
    expected_consumer={'contract':'mariadb-consumer-v1','driver_files':capture['inputs']['driver'],
        'core_go_mod_sha256':capture['inputs']['driver']['go.mod'],
        'consumer_source_sha256':capture['inputs']['workload']['validation/apps/consumer/main.go'],
        'cgo_enabled':False,'workspace':'off','network':'off','build':True,'vet':True,
        'output':'MariaDB Alpha consumer ready','framework_dependencies_present':False,'temporary_export_removed_on_exit':True}
    if consumer!=expected_consumer:raise ValueError('Independent consumer proof differs.')
    return capture


def validate(capture,directory,*,current=True):
    validate_checks(capture,directory,current)
    if capture.get('image_reference')!=compatibility.images().get(capture.get('family')):raise ValueError('Wrong application image.')
    image=capture.get('image',{})
    if image.get('Os')!='linux' or image.get('Architecture') not in ('arm64','amd64') or not re.fullmatch(r'sha256:[0-9a-f]{64}',image.get('Id','')):raise ValueError('Wrong application image identity.')
    provenance.validate_server(capture,Path(directory))
    if not capture['server']['before']['version'].startswith(capture['family']+'.'):raise ValueError('Wrong application server family.')
    if type(capture.get('restart_count')) is not int or capture['restart_count']!=1 or capture.get('resources_cleaned') is not True:raise ValueError('Missing owned application restart/cleanup.')
    previous=capture.get('prior_profile')
    if previous:
        path=(record.HERE/previous['source']).resolve()
        if not path.is_relative_to((record.HERE/'results').resolve()) or record.digest(path.read_bytes())!=previous['sha256']:raise ValueError('Application history source changed.')
        old=json.loads(path.read_text())
        if old['recorded_at']>=capture['recorded_at']:raise ValueError('Application predecessor is not earlier.')
        validate(old,path.parent,current=False)
        if compatibility.profile(old)!=compatibility.profile(capture) or sorted(passing(old))!=previous['passing']:raise ValueError('Application history snapshot differs.')
        if set(previous['passing'])-passing(capture):raise ValueError('Earlier application passes lost.')
    if capture.get('lost_prior_passes')!=[]:raise ValueError('Earlier application passes lost.')
    if current and previous!=prior_profile(capture):raise ValueError('Newest application predecessor was omitted.')
    return capture


def prior_profile(capture):
    eligible=[]
    for path in (record.HERE/'results').rglob('capture.json'):
        if not path.resolve().is_relative_to((record.HERE/'results').resolve()):continue
        try:old=json.loads(path.read_text())
        except (ValueError,OSError):continue
        if (old.get('contract')!=CONTRACT or old.get('native_contract')
                or old.get('exit_code')!=0 or old.get('recorded_at','')>=capture['recorded_at']):continue
        if compatibility.profile(old)!=compatibility.profile(capture):continue
        validate(old,path.parent,current=False)
        eligible.append((old['recorded_at'],path,old))
    if not eligible:return None
    _,path,old=max(eligible,key=lambda entry:entry[0])
    return {'profile':compatibility.profile(old),'source':str(path.relative_to(record.HERE)),
            'sha256':record.digest(path.read_bytes()),'passing':sorted(passing(old))}


def suite():
    return ProfileSuite(CONTRACT,METHOD,required(),commands,assess,validate,prior_profile,passing)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--family',required=True,choices=tuple(compatibility.images()))
    parser.add_argument('--go',default='go');parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args()
    return compatibility.run(args.family,args.go,args.output,suite=suite())


if __name__=='__main__':raise SystemExit(main())
