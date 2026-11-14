#!/usr/bin/env python3
"""Correctness-only MariaDB version profiles with owned Docker resources.

The complete 12.3 authentication/instrumentation suite remains independently
required. This matrix checks older servers without weakening that contract.
Every image is digest-pinned; credentials, datadir and restart control belong
only to this invocation. No benchmark or profiler command is accepted.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import socket
import subprocess
import tempfile
import time

import ci
from cdc_fixture import stop
import correctness
from prepare_ci import certificates
from provenance import read_server, revision_info, server_command
import record
from profile_suite import ProfileSuite

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
CONTRACT = 'mariadb-compatibility-v1'
# A historical receipt must keep its original command contract when today's
# required cases grow. Change the version, not these bytes, for a new command
# contract; earlier passing leaves remain protected across supported versions.
CONTRACT_FILE = HERE/'compatibility-contract-v1.json'
CONTRACT_SHA256 = 'eb304be7e4545175332d84f61df5e9f6d53bff2bea8da137193316bfac6b11dd'
ENVIRONMENT = {'GOMAXPROCS':'2', 'GOFLAGS':'', 'GOWORK':'off', 'GOTOOLCHAIN':'local'}
INTEGRATION = ('TestMariaDBStage7Compatibility','TestMariaDBFailureRecovery',
    'TestMariaDBStage7CDC','TestMariaDBStage7Restart','TestRecoveryFaultBoundary',
    'TestMariaDBBindings','TestMariaDBSQL','TestMariaDBResults','TestMariaDBTransactions',
    'TestMariaDBReliability','TestMariaDBNativeBulk','TestMariaDBNativeUnsignedResult',
    'TestMariaDBSessionObservations','TestMariaDBDefaultSessionPolicy')
# Real KDC/native-provider checks belong to the independently mandatory full
# authentication fixture. They are not an older-server SQL capability probe.
ROOT_REQUIRED = {name for name in ci.REQUIRED if '/validation/' not in name and '/gssapi/' not in name}
INTEGRATION_REQUIRED = {ci.PACKAGE+'/validation/'+name for name in INTEGRATION}
INTEGRATION_REQUIRED |= {ci.PACKAGE+'/validation/TestMariaDBStage7Compatibility/'+name
    for name in ('bulk_units_capability','native_metadata','authentication/ed25519',
                 'authentication/parsec','vector_availability')}
for mode in ('text','compressed','tls','tls_compressed','legacy','uncached'):
    INTEGRATION_REQUIRED.add(ci.PACKAGE+'/validation/TestMariaDBStage7Compatibility/native_policies/'+mode)
for mode in ('text','compressed','tls','tls_compressed'):
    INTEGRATION_REQUIRED.add(ci.PACKAGE+'/validation/TestMariaDBStage7CDC/'+mode)
for operation in ('autocommit','transaction_commit','native_bulk','native_stream'):
    for phase in ('before_write','partial_write','lost_OK'):
        INTEGRATION_REQUIRED.add(ci.PACKAGE+'/validation/TestMariaDBFailureRecovery/'+operation+'/'+phase)


def commands(go):
    return {
        'root':[go,'test','-race','-json','-count=1','-timeout=240s','-p=1','-parallel=2',
                '-tags=mariadb_validation','.', './cdc','./zstd','./internal/...'],
        'integration':[go,'test','-race','-json','-count=1','-timeout=180s','-p=1','-parallel=2',
                       './validation','-run','^('+'|'.join(INTEGRATION)+')$'],
        'vet':[go,'vet','./...'],
    }


def historical_contract():
    raw = CONTRACT_FILE.read_bytes()
    if record.digest(raw) != CONTRACT_SHA256:
        raise ValueError('The original compatibility contract was changed.')
    return json.loads(raw)


def images():
    result = json.loads((HERE/'compatibility-images.json').read_text())
    if set(result) != {'10.11','11.4','11.8','12.3'} or any(
            not re.fullmatch(r'mariadb:'+re.escape(family)+r'@sha256:[0-9a-f]{64}', image)
            for family,image in result.items()):
        raise ValueError('Expected four digest-pinned MariaDB images.')
    return result


def assess(output, code, name, required=None):
    if required is None:required = ROOT_REQUIRED if name == 'root' else INTEGRATION_REQUIRED
    issues,cases = ci.assess_tests(output,code,required=required)
    if name == 'integration':
        markers=[]
        for line in output.splitlines():
            try: event=json.loads(line)
            except ValueError: continue
            text=event.get('Output','')
            if 'MARIADB_COMPATIBILITY ' in text:
                if (event.get('Action')!='output' or event.get('Package')!=ci.PACKAGE+'/validation'
                        or event.get('Test')!='TestMariaDBStage7Compatibility'):
                    issues.append('Misattributed compatibility marker.')
                    continue
                try: markers.append(json.loads(text.split('MARIADB_COMPATIBILITY ',1)[1]))
                except ValueError: issues.append('Malformed compatibility marker.')
        if len(markers)!=1: issues.append('Exactly one live compatibility marker is required.')
    return sorted(set(issues)),cases


def passing(capture):
    return {stage+'/'+name for stage in ('root','integration')
            for name,value in correctness.leaves(capture['stages'][stage]['cases']).items()
            if value.get('status')=='pass'}


def profile(capture):
    return {'family':capture['family'],'version':capture['server']['before']['version'],
            'image_id':capture['image']['Id'],'compiler_sha256':capture['compiler']['sha256'],
            'go':capture['compiler']['version'],'host':capture['host']}


def validate(capture, directory, *, current=True):
    """Corroborate commands, raw events, source, version and owned-server facts."""
    directory=Path(directory)
    if capture.get('contract')!=CONTRACT or capture.get('method')!='compatibility correctness only':
        raise ValueError('Wrong compatibility contract.')
    if capture.get('exit_code')!=0 or not capture.get('inputs_stable') or capture.get('environment')!=ENVIRONMENT:
        raise ValueError('Failed/incomplete compatibility profile.')
    if current and capture.get('inputs')!=record.inputs():
        raise ValueError('Stale compatibility inputs.')
    if capture.get('input_fingerprint')!=record.digest(json.dumps(capture['inputs'],sort_keys=True).encode()):
        raise ValueError('Incorrect compatibility fingerprint.')
    if capture.get('image_reference')!=images().get(capture.get('family')):
        raise ValueError('Wrong matrix image.')
    if not re.fullmatch(r'sha256:[0-9a-f]{64}',capture.get('image',{}).get('Id','')):
        raise ValueError('Missing immutable runtime image identity.')
    if capture['image'].get('Os')!='linux' or capture['image'].get('Architecture') not in ('arm64','amd64'):
        raise ValueError('Wrong runtime image platform.')
    if not re.fullmatch(r'[0-9a-f]{64}',capture.get('compiler',{}).get('sha256','')) or not Path(capture['compiler'].get('path','')).is_absolute():
        raise ValueError('Missing compiler identity.')
    if not re.fullmatch(r'go version go1\.(25|26|27)\.\d+ (darwin|linux)/(arm64|amd64)',capture.get('compiler',{}).get('version','')):
        raise ValueError('Unverified compiler profile.')
    from provenance import validate_server
    validate_server(capture,directory)
    version=capture['server']['before']['version']
    if not version.startswith(capture['family']+'.') or not capture['server'].get('stable'):
        raise ValueError('Wrong/changed server profile.')
    original=historical_contract()
    expected=(commands(capture['compiler']['path']) if current else
              {name:[capture['compiler']['path'],*args] for name,args in original['commands'].items()})
    required=capture.get('required_tests',{})
    current_required={'root':sorted(ROOT_REQUIRED),'integration':sorted(INTEGRATION_REQUIRED)}
    if current and required!=current_required:
        raise ValueError('Required matrix test inventory changed.')
    for stage,minimum in original['required_tests'].items():
        if not isinstance(required.get(stage),list) or not set(minimum).issubset(required[stage]):
            raise ValueError('Incomplete required matrix test inventory.')
    if set(capture.get('stages',{}))!=set(expected):raise ValueError('Missing matrix check stages.')
    if set(capture.get('logs',{}))!={'root.log','integration.log','vet.log','server-before.log','server-after.log','server.log'}:
        raise ValueError('Incomplete compatibility log set.')
    for name,digest in capture['logs'].items():
        if Path(name).name!=name or record.digest((directory/name).read_bytes())!=digest:
            raise ValueError('Compatibility log checksum mismatch.')
    for name,command in expected.items():
        stage=capture['stages'][name]
        if stage.get('command')!=command or stage.get('exit_code')!=0 or stage.get('issues')!=[]:
            raise ValueError('Wrong/failed check command: '+name)
        if name!='vet':
            issues,cases=assess((directory/(name+'.log')).read_text(),stage['exit_code'],name,set(required[name]))
            if issues or cases!=stage.get('cases'):raise ValueError('Compatibility raw test events differ.')
    marker=None
    for line in (directory/'integration.log').read_text().splitlines():
        try:event=json.loads(line)
        except ValueError:continue
        if 'MARIADB_COMPATIBILITY ' in event.get('Output',''):
            marker=json.loads(event['Output'].split('MARIADB_COMPATIBILITY ',1)[1])
    newer=capture['family'] in ('11.8','12.3')
    if marker!={'version':version,'family':capture['family'],
                'expected_features':{'bulk_units':newer,'parsec':newer,'vector':newer}}:
        raise ValueError('Live server feature expectations differ from profile.')
    if type(capture.get('restart_count')) is not int or capture['restart_count']!=1 or capture.get('resources_cleaned') is not True:
        raise ValueError('Owned restart/cleanup evidence is incomplete.')
    prior=capture.get('prior_profile')
    if prior is not None:
        if prior.get('profile')!=profile(capture) or not isinstance(prior.get('passing'),list):
            raise ValueError('Wrong prior profile snapshot.')
        path=(HERE/prior.get('source','')).resolve()
        if not path.is_relative_to((HERE/'results').resolve()) or not path.is_file() or record.digest(path.read_bytes())!=prior.get('sha256'):
            raise ValueError('Prior profile source/hash differs.')
        old=json.loads(path.read_text())
        if old.get('recorded_at','')>=capture.get('recorded_at',''):
            raise ValueError('Prior profile is not earlier.')
        validate(old,path.parent,current=False)
        if profile(old)!=profile(capture) or sorted(passing(old))!=prior['passing']:
            raise ValueError('Prior profile snapshot omits assertions.')
        if set(prior['passing'])-passing(capture):raise ValueError('Earlier matrix passes lost.')
    if capture.get('lost_prior_passes')!=[]:raise ValueError('Earlier matrix passes lost.')
    if current and capture.get('prior_profile')!=prior_profile(capture):
        raise ValueError('Newest prior profile was omitted or changed.')
    return capture


def prior_profile(capture):
    candidates=[]
    results=(HERE/'results').resolve()
    for path in results.rglob('capture.json'):
        if not path.resolve().is_relative_to(results):
            continue
        try:
            old=json.loads(path.read_text())
            if old.get('recorded_at','')>=capture.get('recorded_at',''):continue
            if old.get('contract')!=CONTRACT or profile(old)!=profile(capture):continue
        except (ValueError,KeyError,OSError):continue
        if old.get('exit_code')!=0:
            continue  # Failed attempts are retained, but cannot establish passes.
        if not isinstance(old.get('required_tests'),dict):
            # Early development captures predate the verifiable v1 inventory.
            # They never established a qualified predecessor under this contract.
            continue
        # A matching successful receipt becoming invalid must not silently erase
        # history. Report corruption instead of quietly starting a new baseline.
        try:
            validate(old,path.parent,current=False)
        except (ValueError,KeyError,OSError) as error:
            raise EvidenceFailure('Invalid earlier compatibility profile: '+str(path)) from error
        candidates.append((old.get('recorded_at',''),path,old))
    if not candidates:return None
    _,path,old=max(candidates,key=lambda item:item[0])
    return {'profile':profile(old),'passing':sorted(passing(old)),
            'source':str(path.relative_to(HERE.resolve())),'sha256':record.digest(path.read_bytes())}


class EvidenceFailure(ValueError):
    """An evidence path and a fixed validation reason contain no fixture secrets."""
    def __init__(self, diagnostic):
        super().__init__(diagnostic)
        self.details = {'operation':'pass_history', 'status':'invalid_evidence', 'diagnostic':diagnostic}


class FixtureFailure(RuntimeError):
    """Only explicitly sanitized tool output may enter a public failure receipt."""
    def __init__(self, operation, status, diagnostic):
        super().__init__('Owned fixture operation failed: '+operation)
        self.details = {'operation':operation, 'status':status, 'diagnostic':diagnostic[-8192:]}


def failure_details(error, phase):
    # Arbitrary exceptions can include SQL, subprocess environments or secrets.
    # Retain their type and our phase; richer text is limited to our safe wrapper.
    details = {'phase':phase, 'error_type':type(error).__name__}
    if isinstance(error, (FixtureFailure, EvidenceFailure)):
        details.update(error.details)
    return details


def save_failure(capture, directory, error, phase, key='failure'):
    details = failure_details(error, phase)
    capture[key] = details
    try:
        (directory / (key+'.log')).write_text(json.dumps(details, indent=2)+'\n')
    except OSError as write_error:
        # Diagnostics are best effort. A full disk or permission error must
        # never prevent the caller from releasing owned Docker resources.
        details['write_error_type'] = type(write_error).__name__


class OwnedServer:
    """Only random, labelled resources allocated by this instance may restart/delete."""
    def __init__(self, directory, reference):
        self.directory=directory;self.reference=reference
        self.token=secrets.token_hex(10);self.name='go-mariadb-compat-'+self.token
        self.volume=self.name+'-data';self.password=secrets.token_hex(24)
        self.created_volume=False;self.created_container=False;self.restarts=0
        self.client=shutil.which('mariadb')
        if not self.client:raise ValueError('MariaDB CLI is required for matrix provenance.')

    def docker(self,*args,timeout=60,check=True):
        operation='docker.'+args[0]
        if args[0] in ('volume','image'):
            operation+='.'+args[1]
        def redact(*values):
            text=''.join(value.decode(errors='replace') if isinstance(value,bytes) else value or ''
                         for value in values)
            return text.replace(self.password,'[redacted]')
        try:
            result=subprocess.run(['docker',*args],capture_output=True,text=True,timeout=timeout)
        except subprocess.TimeoutExpired as error:
            raise FixtureFailure(operation,'timeout',redact(error.stdout,error.stderr)) from None
        except OSError as error:
            raise FixtureFailure(operation,type(error).__name__,'Docker could not be executed.') from None
        if check and result.returncode:
            raise FixtureFailure(operation,result.returncode,redact(result.stdout,result.stderr))
        return result

    def inspect(self):return json.loads(self.docker('inspect',self.name).stdout)[0]

    def owns_container(self):
        info=self.inspect()
        if info['Config'].get('Labels',{}).get('go-mariadb.validation.owner')!=self.token:
            raise RuntimeError('Refusing a container without this invocation ownership label.')
        return info

    def logs(self):
        # A failed docker run can leave a name collision. Ownership must be
        # checked before reading diagnostics, as well as before restart/delete.
        self.owns_container()
        result=self.docker('logs',self.name,check=False)
        return (result.stdout+result.stderr).replace(self.password,'[redacted]')

    def sql(self,text):
        command=[self.client,'--no-defaults','--protocol=tcp','--host=127.0.0.1',
                 '--port='+str(self.port),'--user=root','--skip-ssl','--batch','--skip-column-names']
        result=subprocess.run(command,input=text,capture_output=True,text=True,
                              env={**os.environ,'MYSQL_PWD':self.password},timeout=10)
        if result.returncode:
            raise FixtureFailure('sql',result.returncode,(result.stdout+result.stderr).replace(self.password,'[redacted]'))
        return result.stdout.strip()

    def ready(self):
        deadline=time.monotonic()+60
        while time.monotonic()<deadline:
            try:
                if self.sql('SELECT 1')=='1':return
            except (RuntimeError,subprocess.TimeoutExpired):pass
            info=self.owns_container()
            if not info['State']['Running']:raise RuntimeError('Owned MariaDB stopped before readiness.')
            time.sleep(0.1)
        raise RuntimeError('Owned MariaDB readiness deadline.')

    def start(self):
        tls=self.directory/'tls';certificates(tls)
        # The private parent stays 0700. Only server certificates/keys are
        # mounted read-only; container's mariadbd UID must be able to read them.
        tls.chmod(0o755)
        for name in ('ca.pem','server-cert.pem','server-key.pem'):(tls/name).chmod(0o644)
        public=self.directory/'server-tls';public.mkdir(mode=0o755);public.chmod(0o755)
        for name in ('ca.pem','server-cert.pem','server-key.pem'):
            shutil.copyfile(tls/name,public/name);(public/name).chmod(0o644)
        envfile=self.directory/'server.env'
        fd=os.open(envfile,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
        with os.fdopen(fd,'w') as output:output.write('MARIADB_ROOT_PASSWORD='+self.password+'\nMARIADB_ROOT_HOST=%\n')
        image=self.docker('image','inspect',self.reference,check=False)
        if image.returncode:
            self.docker('pull',self.reference,timeout=300)
            image=self.docker('image','inspect',self.reference)
        self.image=json.loads(image.stdout)[0]
        self.created_volume=True
        self.docker('volume','create','--label','go-mariadb.validation.owner='+self.token,self.volume)
        # Mark the attempted name before launch: a failed start can leave a
        # labelled container which still needs removal. No host datadir is used.
        self.created_container=True
        # Docker may allocate a different ephemeral host port on restart.
        # Select one available loopback port and explicitly retain that binding;
        # a bind race fails this owned attempt rather than changing endpoints.
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1',0));selected_port=reservation.getsockname()[1]
        self.docker('run','--detach','--name',self.name,'--label','go-mariadb.validation.owner='+self.token,
            '--cpus=2','--memory=1g','--publish','127.0.0.1:'+str(selected_port)+':3306','--env-file',str(envfile),
            '--mount','type=volume,src='+self.volume+',dst=/var/lib/mysql',
            '--mount','type=bind,src='+str(public)+',dst=/fixture,readonly',self.reference,
            '--server-id=731700','--log-bin=/var/lib/mysql/bin','--binlog-format=ROW',
            '--binlog-row-image=FULL','--binlog-row-metadata=FULL','--binlog-checksum=CRC32',
            '--performance-schema=ON','--max-allowed-packet=48M','--local-infile=1',
            '--innodb-buffer-pool-size=64M','--max-connections=64',
            '--ssl-ca=/fixture/ca.pem','--ssl-cert=/fixture/server-cert.pem','--ssl-key=/fixture/server-key.pem',
            '--default-time-zone=+00:00')
        info=self.owns_container()
        if info['Image']!=self.image['Id']:raise RuntimeError('Container used wrong image.')
        self.port=int(info['NetworkSettings']['Ports']['3306/tcp'][0]['HostPort'])
        self.ready()
        version=self.sql('SELECT VERSION()')
        parts=version.split('.')
        if len(parts)<3 or 'MariaDB' not in version:raise RuntimeError('Expected MariaDB version.')
        self.sql('CREATE DATABASE go_mariadb_validation CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE DATABASE go_mariadb_upstream CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci')
        plugins={'ed25519':'auth_ed25519'}
        if tuple(map(int,parts[:2]))>=(11,6):plugins['parsec']='auth_parsec'
        for plugin,library in plugins.items():
            if self.sql("SELECT COUNT(*) FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_STATUS='ACTIVE' AND PLUGIN_NAME='"+plugin+"'")=='0':
                self.sql("INSTALL SONAME '"+library+"'")
            self.sql("CREATE USER 'gomdb_"+plugin+"'@'%' IDENTIFIED VIA "+plugin+" USING PASSWORD('"+self.password+"'); GRANT ALL ON go_mariadb_validation.* TO 'gomdb_"+plugin+"'@'%'")
        self.config={'host':'127.0.0.1','port':self.port,'socket':'','user':'root','password':self.password,
            'database':'go_mariadb_validation','upstream_database':'go_mariadb_upstream',
            'tls_ca':str(tls/'ca.pem'),'tls_cert':str(tls/'client-cert.pem'),'tls_key':str(tls/'client-key.pem'),
            'plugins':sorted(plugins)}
        path=self.directory/'config.json';fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
        with os.fdopen(fd,'w') as output:json.dump(self.config,output)
        self.config_path=path
        return self

    def restart(self):
        if self.restarts>=1:raise RuntimeError('Unexpected duplicate owned restart request.')
        self.owns_container();self.docker('restart','--time','3',self.name,timeout=45)
        info=self.owns_container()
        if int(info['NetworkSettings']['Ports']['3306/tcp'][0]['HostPort'])!=self.port:
            raise RuntimeError('Owned restart changed the endpoint.')
        self.ready();self.restarts+=1

    def close(self):
        try:
            if self.created_container:
                found=self.docker('inspect',self.name,check=False)
                if found.returncode==0:
                    self.owns_container();self.docker('rm','--force','--volumes',self.name)
        finally:
            if self.created_volume:
                found=self.docker('volume','inspect',self.volume,check=False)
                if found.returncode:
                    if 'No such volume' not in found.stderr:
                        raise RuntimeError('Cannot inspect owned volume for cleanup.')
                else:
                    result=json.loads(found.stdout)[0]
                    if result.get('Labels',{}).get('go-mariadb.validation.owner')!=self.token:
                        raise RuntimeError('Refusing a volume without this invocation ownership label.')
                    self.docker('volume','rm',self.volume)


def run_stage(command, env, fixture, control):
    with tempfile.TemporaryFile(mode='w+') as output:
        child=subprocess.Popen(command,cwd=ROOT,env=env,stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
        failure=None
        diagnostic=None
        try:
            deadline=time.monotonic()+300
            while child.poll() is None:
                if time.monotonic()>deadline:raise RuntimeError('Compatibility child deadline.')
                request=control/'restart.request'
                if request.exists():
                    if request.read_bytes():raise RuntimeError('Malformed owned restart request.')
                    request.unlink();fixture.restart();(control/'restart.done').touch()
                time.sleep(0.025)
        except (Exception,KeyboardInterrupt) as error:
            failure=type(error).__name__
            diagnostic=failure_details(error,'check_controller')
        finally:
            try:stop(child,group=True)
            except Exception as error:
                failure=type(error).__name__
                diagnostic=failure_details(error,'check_cleanup')
        output.seek(0)
        text=output.read().replace(fixture.password,'[redacted]')
        if failure:
            text+='\nOwned fixture controller failed: '+failure+'\n'
            if diagnostic is not None:text+=json.dumps(diagnostic)+'\n'
        return (1 if failure else child.returncode),text


def run(family,go,directory,*,suite=None):
    # Application profiles use the same ownership/redaction/restart machinery,
    # with an independent command and evidence contract. No globals are patched.
    if suite is None:
        suite=ProfileSuite(CONTRACT,'compatibility correctness only',
            {'root':sorted(ROOT_REQUIRED),'integration':sorted(INTEGRATION_REQUIRED)},
            commands,lambda output,code,name: assess(output,code,name) if name!='vet' else (['vet failed'] if code else [],{}),
            validate,prior_profile,passing)
    directory=Path(directory).resolve()
    if not directory.is_relative_to((HERE/'results').resolve()):
        raise ValueError('Compatibility output must be within validation/results for pass-history discovery.')
    if directory.exists():raise ValueError('Retain prior attempts; output must be a new directory.')
    directory.mkdir(parents=True)
    source=record.inputs();env={**os.environ,**ENVIRONMENT}
    capture={'contract':suite.contract,'method':suite.method,'family':family,'source_directory':str(ROOT),
        'image_reference':images()[family],'host':platform.platform(),
        'recorded_at':datetime.now(timezone.utc).isoformat(),**revision_info(ROOT),
        'inputs':source,'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
        'environment':ENVIRONMENT,'stages':{},'logs':{},'exit_code':1,'inputs_stable':False,
        'required_tests':suite.required,
        'restart_count':0,'resources_cleaned':False,'lost_prior_passes':[],'prior_profile':None}
    with tempfile.TemporaryDirectory(prefix='go-mariadb-compat-') as temporary:
        private=Path(temporary);private.chmod(0o700)
        fixture=None
        phase='compiler_identity'
        try:
            binary=Path(shutil.which(go) or go).resolve()
            compiler={'path':str(binary),'sha256':record.digest(binary.read_bytes()),
                      'version':subprocess.check_output([str(binary),'version'],text=True,env=env,timeout=10).strip()}
            capture['compiler']=compiler
            phase='fixture_initialization'
            fixture=OwnedServer(private,images()[family])
            phase='fixture_setup'
            fixture.start()
            capture['image']={key:fixture.image[key] for key in ('Id','Architecture','Os')}
            control=private/'control';control.mkdir(mode=0o700)
            env.update(MARIADB_VALIDATION_CONFIG=str(fixture.config_path),MYSQL_TEST_USER='root',
                MYSQL_TEST_PASS=fixture.password,MYSQL_TEST_DBNAME='go_mariadb_upstream',
                MYSQL_TEST_ADDR='127.0.0.1:'+str(fixture.port),GO_MARIADB_MATRIX_FAMILY=family,
                GO_MARIADB_MATRIX_CONTROL=str(control))
            for key in ('MYSQL_TEST_CONCURRENT','MYSQL_ZSTD_TEST_DSN','GO_MARIADB_REQUIRE_INSTRUMENTATION',
                        'GO_MARIADB_CDC_SOCKET','GO_MARIADB_CDC_CONTROL'):env.pop(key,None)
            phase='server_provenance_before'
            command=server_command(fixture.config,fixture.client)
            before,raw=read_server(fixture.config,env,command)
            (directory/'server-before.log').write_text(raw)
            capture['server']={'endpoint':{k:fixture.config[k] for k in ('host','port','user','database')},
                               'command':command,'before':before}
            phase='pass_history'
            capture['prior_profile']=suite.history(capture)
            for name,argv in suite.commands(str(binary)).items():
                phase='check_'+name
                code,output=run_stage(argv,env,fixture,control)
                (directory/(name+'.log')).write_text(output)
                issues,cases=suite.assess(output,code,name)
                capture['stages'][name]={'command':argv,'exit_code':code,'issues':issues,'cases':cases}
                print(f'{family} {compiler["version"]}: {name} exit={code}, issues={len(issues)}',flush=True)
                if code or issues:raise RuntimeError('Compatibility check failed: '+name)
            phase='server_provenance_after'
            after,raw=read_server(fixture.config,env,command)
            (directory/'server-after.log').write_text(raw)
            capture['server'].update(after=after,stable=before==after)
            prior=capture['prior_profile']
            capture['lost_prior_passes']=sorted(set(prior['passing'])-suite.passing(capture)) if prior else []
            capture['inputs_stable']=source==record.inputs()
            capture['exit_code']=0
        except (Exception,KeyboardInterrupt) as error:
            capture['error_type']=type(error).__name__
            save_failure(capture,directory,error,phase)
            capture['exit_code']=1
            print('Compatibility profile failed; inspect '+str(directory/'failure.log')+'.',flush=True)
        finally:
            try:
                if fixture is not None and fixture.created_container:
                    (directory/'server.log').write_text(fixture.logs())
            except Exception as error:
                capture['server_log_error_type']=type(error).__name__;capture['exit_code']=1
                save_failure(capture,directory,error,'server_diagnostics','server_log_failure')
            try:
                if fixture is not None:fixture.close()
                capture['resources_cleaned']=True
            except Exception as error:
                capture['cleanup_error_type']=type(error).__name__;capture['exit_code']=1
                save_failure(capture,directory,error,'fixture_cleanup','cleanup_failure')
            capture['restart_count']=fixture.restarts if fixture is not None else 0
            capture['inputs_stable']=source==record.inputs()
            capture['logs']={p.name:record.digest(p.read_bytes()) for p in directory.glob('*.log')}
            (directory/'capture.json').write_text(json.dumps(capture,indent=2)+'\n')
    if capture['exit_code']==0:
        try:suite.validate(capture,directory)
        except (ValueError,KeyError,OSError) as error:
            capture['validation_error_type']=type(error).__name__;capture['exit_code']=1
            (directory/'capture.json').write_text(json.dumps(capture,indent=2)+'\n')
            raise
    return capture['exit_code']


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--family',required=True,choices=tuple(images()))
    parser.add_argument('--go',default='go')
    parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args()
    return run(args.family,args.go,args.output)


if __name__=='__main__':raise SystemExit(main())
