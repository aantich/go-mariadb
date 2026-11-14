"""Version profiles must prove required work and retain failure/cleanup evidence."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import compatibility as matrix
import provenance
import record
from test_provenance import sample_server


class CompatibilityEvidenceTests(unittest.TestCase):
    def valid(self,directory):
        source=record.inputs();server=sample_server()
        endpoint={'host':'127.0.0.1','port':3307,'user':'root','database':'go_mariadb_validation'}
        capture={'contract':matrix.CONTRACT,'method':'compatibility correctness only','family':'12.3',
            'recorded_at':'2026-11-12T22:00:00+00:00','image_reference':matrix.images()['12.3'],
            'image':{'Id':'sha256:'+'a'*64,'Architecture':'arm64','Os':'linux'},
            'compiler':{'path':'/synthetic/bin/go','sha256':'b'*64,'version':'go version go1.27.4 linux/arm64'},
            'host':'synthetic','environment':dict(matrix.ENVIRONMENT),'inputs':source,
            'required_tests':{'root':sorted(matrix.ROOT_REQUIRED),'integration':sorted(matrix.INTEGRATION_REQUIRED)},
            'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
            'inputs_stable':True,'exit_code':0,'restart_count':1,'resources_cleaned':True,
            'lost_prior_passes':[],'prior_profile':None,'logs':{},'stages':{},
            'server':{'endpoint':endpoint,'command':provenance.server_command(endpoint,'mariadb'),
                      'before':server.copy(),'after':server.copy(),'stable':True}}
        for name,command in matrix.commands(capture['compiler']['path']).items():
            events=[]
            if name!='vet':
                required=matrix.ROOT_REQUIRED if name=='root' else matrix.INTEGRATION_REQUIRED
                for key in sorted(required):
                    package,_,test=key.partition('/Test')
                    events.append({'Action':'pass','Package':package,'Test':'Test'+test})
                if name=='integration':
                    marker={'version':server['version'],'family':'12.3','expected_features':{'bulk_units':True,'parsec':True,'vector':True}}
                    events.append({'Action':'output','Package':matrix.ci.PACKAGE+'/validation',
                                   'Test':'TestMariaDBStage7Compatibility','Output':'MARIADB_COMPATIBILITY '+json.dumps(marker)})
            output='\n'.join(json.dumps(event) for event in events)+'\n'
            (directory/(name+'.log')).write_text(output)
            issues,cases=matrix.assess(output,0,name) if name!='vet' else ([],{})
            self.assertEqual(issues,[])
            capture['stages'][name]={'command':command,'exit_code':0,'issues':issues,'cases':cases}
        for phase in ('before','after'):(directory/('server-'+phase+'.log')).write_text(json.dumps(server)+'\n')
        (directory/'server.log').write_text('owned server stopped\n')
        self.rehash(capture,directory)
        return capture

    def rehash(self,capture,directory):
        capture['logs']={p.name:record.digest(p.read_bytes()) for p in directory.glob('*.log')}

    def test_complete_profile_and_protected_required_cases(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);capture=self.valid(directory)
            matrix.validate(capture,directory)
            original=(directory/'integration.log').read_text()
            for missing in ('native_policies/text','bulk_units_capability','native_stream/lost_OK','TestMariaDBStage7Restart'):
                with self.subTest(missing=missing):
                    output='\n'.join(line for line in original.splitlines() if missing not in line)+'\n'
                    issues,_=matrix.assess(output,0,'integration')
                    self.assertTrue(issues)

    def test_wrong_sources_commands_runtime_and_cleanup_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);capture=self.valid(directory)
            for key,value in (('inputs',{}),('input_fingerprint','wrong'),('family','11.4'),
                              ('resources_cleaned',False),('restart_count',0),('restart_count',True),
                              ('image_reference','mariadb:latest'),('environment',{}),('exit_code',1)):
                changed=copy.deepcopy(capture);changed[key]=value
                with self.subTest(key=key,value=value),self.assertRaises(ValueError):matrix.validate(changed,directory)
            for argv in (['go','test','-bench=.'],['go','test','-benchtime=1x'],['go','test']):
                changed=copy.deepcopy(capture);changed['stages']['root']['command']=argv
                with self.assertRaises(ValueError):matrix.validate(changed,directory)

    def test_raw_events_and_unavailable_feature_expectations_are_verified(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);capture=self.valid(directory)
            original=(directory/'integration.log').read_text()
            marker=next(line for line in original.splitlines() if 'MARIADB_COMPATIBILITY ' in line)
            variants=[original+'\n'+marker,original.replace('MARIADB_COMPATIBILITY ','MISSING '),
                      original.replace('\\"bulk_units\\": true','\\"bulk_units\\": false')]
            for output in variants:
                changed=copy.deepcopy(capture);(directory/'integration.log').write_text(output);self.rehash(changed,directory)
                _,cases=matrix.assess(output,0,'integration');changed['stages']['integration']['cases']=cases
                with self.assertRaises(ValueError):matrix.validate(changed,directory)
            event=json.loads(marker);event['Test']='TestUnrelated'
            self.assertTrue(matrix.assess(original.replace(marker,json.dumps(event)),0,'integration')[0])

    def test_skips_and_data_races_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);self.valid(directory)
            output=(directory/'integration.log').read_text()
            self.assertTrue(matrix.assess(output.replace('"Action": "pass"','"Action": "skip"',1),0,'integration')[0])
            self.assertTrue(matrix.assess(output+'\nWARNING: DATA RACE\n',0,'integration')[0])

    def test_matrix_source_locks_are_fingerprinted_and_immutable(self):
        source=record.inputs()['workload']
        self.assertIn('validation/compatibility.py',source)
        self.assertIn('validation/compatibility-images.json',source)
        self.assertIn('validation/compatibility-contract-v1.json',source)
        self.assertEqual(set(matrix.images()),{'10.11','11.4','11.8','12.3'})
        self.assertTrue(all('@sha256:' in value for value in matrix.images().values()))
        self.assertNotIn('./gssapi',matrix.commands('/bin/go')['root'])
        self.assertTrue(any('/gssapi/' in name for name in matrix.ci.REQUIRED))

    def test_prior_profile_and_newer_passing_leaf_cannot_be_omitted(self):
        # Source exports and Linux mounts are read-only. Give discovery its own
        # synthetic validation root, rather than writing beneath the checkout.
        locked_images=(matrix.HERE/'compatibility-images.json').read_bytes()
        with tempfile.TemporaryDirectory(prefix='go-mariadb-stage7-harness-') as tmp, \
             mock.patch.object(matrix,'HERE',Path(tmp)):
            (matrix.HERE/'compatibility-images.json').write_bytes(locked_images)
            root=matrix.HERE/'results'/'future-compatibility-stage';root.mkdir(parents=True)
            older=root/'older';current=root/'current';older.mkdir();current.mkdir()
            before=self.valid(older);before['recorded_at']='2026-11-12T21:00:00+00:00'
            extra={'Action':'pass','Package':matrix.ci.PACKAGE,'Test':'TestStage7EarlierPassingLeaf'}
            def append_case(capture,directory):
                with (directory/'root.log').open('a') as output:output.write(json.dumps(extra)+'\n')
                _,capture['stages']['root']['cases']=matrix.assess((directory/'root.log').read_text(),0,'root')
                self.rehash(capture,directory)
            append_case(before,older)
            (older/'capture.json').write_text(json.dumps(before,indent=2)+'\n')
            after=self.valid(current);append_case(after,current)
            after['prior_profile']=matrix.prior_profile(after)
            self.assertIsNotNone(after['prior_profile'])
            matrix.validate(after,current)
            changed=copy.deepcopy(after);changed['prior_profile']=None
            with self.assertRaisesRegex(ValueError,'prior profile was omitted'):matrix.validate(changed,current)
            changed=copy.deepcopy(after);changed['prior_profile']['passing']=[]
            with self.assertRaisesRegex(ValueError,'omits assertions'):matrix.validate(changed,current)
            output=(current/'root.log').read_text()
            (current/'root.log').write_text('\n'.join(line for line in output.splitlines() if 'TestStage7EarlierPassingLeaf' not in line)+'\n')
            _,after['stages']['root']['cases']=matrix.assess((current/'root.log').read_text(),0,'root')
            self.rehash(after,current)
            with self.assertRaisesRegex(ValueError,'Earlier matrix passes lost'):matrix.validate(after,current)

    def test_inventory_growth_preserves_older_passes_and_commands(self):
        locked_images=(matrix.HERE/'compatibility-images.json').read_bytes()
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(matrix,'HERE',Path(tmp)):
            (matrix.HERE/'compatibility-images.json').write_bytes(locked_images)
            older=matrix.HERE/'results'/'any-name'/'older';older.mkdir(parents=True)
            current=older.parent/'current';current.mkdir()
            before=self.valid(older);before['recorded_at']='2026-11-12T21:00:00+00:00'
            extra={'Action':'pass','Package':matrix.ci.PACKAGE,'Test':'TestEarlierPassingLeaf'}
            with (older/'root.log').open('a') as output:output.write(json.dumps(extra)+'\n')
            _,before['stages']['root']['cases']=matrix.assess((older/'root.log').read_text(),0,'root')
            self.rehash(before,older)
            (older/'capture.json').write_text(json.dumps(before))
            prototype=older.parent/'unqualified-development-attempt';prototype.mkdir()
            early=copy.deepcopy(before);early.pop('required_tests')
            (prototype/'capture.json').write_text(json.dumps(early))
            required=matrix.INTEGRATION_REQUIRED|{matrix.ci.PACKAGE+'/validation/TestMariaDBStage7Compatibility/future_required_case'}
            with mock.patch.object(matrix,'INTEGRATION_REQUIRED',required):
                after=self.valid(current)
                after['prior_profile']=matrix.prior_profile(after)
                self.assertIsNotNone(after['prior_profile'])
                with self.assertRaisesRegex(ValueError,'Earlier matrix passes lost'):
                    matrix.validate(after,current)
                with (current/'root.log').open('a') as output:output.write(json.dumps(extra)+'\n')
                _,after['stages']['root']['cases']=matrix.assess((current/'root.log').read_text(),0,'root')
                self.rehash(after,current)
                matrix.validate(after,current)
                # Historical command verification also stays independent of a
                # later collector command change. New command contracts need a
                # new version, rather than rewriting the old receipt's meaning.
                with mock.patch.object(matrix,'commands',return_value={}):
                    matrix.validate(before,older,current=False)
            # Corruption of a successful predecessor must fail closed instead
            # of treating that predecessor as an absent baseline.
            (older/'root.log').write_text('corrupted\n')
            with self.assertRaisesRegex(ValueError,'Invalid earlier compatibility profile'):
                matrix.prior_profile(after)

    def test_original_contract_cannot_be_rewritten(self):
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'contract.json';path.write_text('{}')
            with mock.patch.object(matrix,'CONTRACT_FILE',path),self.assertRaisesRegex(ValueError,'original compatibility contract'):
                matrix.historical_contract()


class CompatibilityOwnershipTests(unittest.TestCase):
    def fixture(self,directory):
        with mock.patch.object(matrix.shutil,'which',return_value='/synthetic/mariadb'):
            return matrix.OwnedServer(directory,matrix.images()['12.3'])

    def test_foreign_container_is_never_removed_and_owned_volume_is_still_cleaned(self):
        with tempfile.TemporaryDirectory() as tmp:
            fixture=self.fixture(Path(tmp));fixture.created_container=fixture.created_volume=True
            calls=[]
            def docker(*args,**kwargs):
                calls.append(args)
                if args[0]=='inspect':data=[{'Config':{'Labels':{'go-mariadb.validation.owner':'someone-else'}}}]
                else:data=[{'Labels':{'go-mariadb.validation.owner':fixture.token}}]
                return subprocess.CompletedProcess(args,0,json.dumps(data),'')
            with mock.patch.object(fixture,'docker',side_effect=docker),self.assertRaisesRegex(RuntimeError,'Refusing a container'):
                fixture.close()
            self.assertFalse(any(args[0]=='rm' for args in calls))
            self.assertIn(('volume','rm',fixture.volume),calls)

    def test_foreign_volume_and_duplicate_restart_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            fixture=self.fixture(Path(tmp));fixture.created_volume=True
            inspect=subprocess.CompletedProcess([],0,json.dumps([{'Labels':{}}]),'')
            with mock.patch.object(fixture,'docker',return_value=inspect) as docker,self.assertRaisesRegex(RuntimeError,'Refusing a volume'):
                fixture.close()
            self.assertEqual(docker.call_count,1)
            fixture.restarts=1
            with mock.patch.object(fixture,'docker') as docker,self.assertRaisesRegex(RuntimeError,'duplicate'):
                fixture.restart()
            docker.assert_not_called()

    def test_failed_start_never_reads_foreign_container_logs(self):
        with tempfile.TemporaryDirectory() as tmp:
            fixture=self.fixture(Path(tmp))
            inspect=subprocess.CompletedProcess([],0,json.dumps([{'Config':{'Labels':{}}}]),'')
            with mock.patch.object(fixture,'docker',return_value=inspect) as docker, \
                 self.assertRaisesRegex(RuntimeError,'Refusing a container'):
                fixture.logs()
            self.assertEqual(docker.call_args_list,[mock.call('inspect',fixture.name)])

    def test_controller_failure_retains_output_redacts_secret_and_reaps_child(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);fixture=mock.Mock(password='synthetic-private-password')
            fixture.restart.side_effect=RuntimeError('restart failed')
            code='import pathlib,time\nprint("diagnostic synthetic-private-password",flush=True)\npathlib.Path('+repr(str(directory/'restart.request'))+').touch()\ntime.sleep(30)\n'
            status,output=matrix.run_stage([sys.executable,'-c',code],os.environ.copy(),fixture,directory)
            self.assertEqual(status,1)
            self.assertIn('diagnostic [redacted]',output)
            self.assertNotIn(fixture.password,output)
            self.assertIn('controller failed: RuntimeError',output)
            fixture.restart.assert_called_once()

    def test_setup_failure_retains_redacted_docker_diagnostics(self):
        locked_images=(matrix.HERE/'compatibility-images.json').read_bytes()
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(matrix,'HERE',Path(tmp)):
            root=Path(tmp);(root/'compatibility-images.json').write_bytes(locked_images)
            fixture=self.fixture(root)
            secret=fixture.password
            result=subprocess.CompletedProcess([],1,'registry unavailable '+secret,'DNS lookup failed '+secret)
            def start():fixture.docker('pull',fixture.reference)
            with mock.patch.object(matrix,'OwnedServer',return_value=fixture), \
                 mock.patch.object(fixture,'start',side_effect=start), \
                 mock.patch.object(matrix.platform,'platform',return_value='synthetic'), \
                 mock.patch.object(matrix.subprocess,'check_output',return_value='go version go1.27.4 linux/arm64'), \
                 mock.patch.object(matrix.subprocess,'run',return_value=result):
                target=root/'results'/'arbitrary-output-name'
                self.assertEqual(matrix.run('12.3',sys.executable,target),1)
            capture=json.loads((target/'capture.json').read_text())
            self.assertEqual(capture['failure']['phase'],'fixture_setup')
            self.assertEqual(capture['failure']['operation'],'docker.pull')
            self.assertIn('DNS lookup failed [redacted]',capture['failure']['diagnostic'])
            self.assertIn('failure.log',capture['logs'])
            self.assertFalse((target/'server.log').exists())
            for path in target.iterdir():self.assertNotIn(secret,path.read_text())

    def test_timeout_and_execution_failure_have_safe_operation_details(self):
        with tempfile.TemporaryDirectory() as tmp:
            fixture=self.fixture(Path(tmp))
            timeout=subprocess.TimeoutExpired(['docker'],1,output=b'partial '+fixture.password.encode(),stderr=b'network failed')
            for error,status in ((timeout,'timeout'),(FileNotFoundError('private text'),'FileNotFoundError')):
                with self.subTest(status=status),mock.patch.object(matrix.subprocess,'run',side_effect=error):
                    with self.assertRaises(matrix.FixtureFailure) as caught:fixture.docker('pull',fixture.reference)
                    details=matrix.failure_details(caught.exception,'fixture_setup')
                    self.assertEqual(details['operation'],'docker.pull')
                    self.assertEqual(details['status'],status)
                    self.assertNotIn(fixture.password,json.dumps(details))
                    self.assertNotIn('private text',json.dumps(details))

    def test_arbitrary_exception_does_not_disclose_its_message(self):
        details=matrix.failure_details(RuntimeError('secret SQL password'),'fixture_setup')
        self.assertEqual(details,{'phase':'fixture_setup','error_type':'RuntimeError'})

    def test_diagnostic_write_failure_cannot_interrupt_cleanup(self):
        capture={}
        with mock.patch.object(Path,'write_text',side_effect=OSError('full disk')):
            matrix.save_failure(capture,Path('/synthetic'),RuntimeError('private text'),'fixture_cleanup')
        self.assertEqual(capture['failure']['phase'],'fixture_cleanup')
        self.assertEqual(capture['failure']['write_error_type'],'OSError')
        self.assertNotIn('private text',json.dumps(capture))

    def test_preflight_failures_also_leave_a_receipt(self):
        locked_images=(matrix.HERE/'compatibility-images.json').read_bytes()
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(matrix,'HERE',Path(tmp)):
            root=Path(tmp);(root/'compatibility-images.json').write_bytes(locked_images)
            missing=root/'results'/'missing-compiler'
            self.assertEqual(matrix.run('12.3',str(root/'absent-go'),missing),1)
            capture=json.loads((missing/'capture.json').read_text())
            self.assertEqual(capture['failure']['phase'],'compiler_identity')
            with mock.patch.object(matrix,'OwnedServer',side_effect=ValueError('private secret')), \
                 mock.patch.object(matrix.subprocess,'check_output',return_value='go version go1.27.4 linux/arm64'), \
                 mock.patch.object(matrix.platform,'platform',return_value='synthetic'):
                target=root/'results'/'missing-client'
                self.assertEqual(matrix.run('12.3',sys.executable,target),1)
            capture=json.loads((target/'capture.json').read_text())
            self.assertEqual(capture['failure']['phase'],'fixture_initialization')
            self.assertTrue(capture['resources_cleaned'])
            self.assertNotIn('private secret',(target/'failure.log').read_text())
