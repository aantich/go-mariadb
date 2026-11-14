"""Application receipts must prove actual work, locked sources and recovery."""
import copy
import json
import os
import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import applications as apps
import applications_cohort as cohort
import applications_local as native
import compatibility
import provenance
import record
from test_provenance import sample_server


class ApplicationEvidenceTests(unittest.TestCase):
    def setUp(self):
        source=record.inputs();original=record.HERE
        root=Path(self.enterContext(tempfile.TemporaryDirectory())).resolve()
        self.enterContext(mock.patch.object(record,'HERE',root))
        self.enterContext(mock.patch.object(record,'inputs',return_value=source))
        (root/'apps').mkdir()
        for name in ('applications-contract-v1.json','apps/dependencies.json'):
            (root/name).write_bytes((original/name).read_bytes())
        self.directory=root/'results/profile';self.directory.mkdir(parents=True)
        self.capture=self.valid(self.directory)

    def valid(self,directory,compiler='1.27',family='12.3'):
        source=record.inputs();server=sample_server();server['version']=family+'.1-MariaDB'
        endpoint={'host':'127.0.0.1','port':3307,'user':'root','database':'go_mariadb_validation'}
        capture={'contract':apps.CONTRACT,'method':apps.METHOD,'family':family,
            'source_directory':str(record.ROOT),'recorded_at':'2026-11-14T01:00:00+00:00',
            'image_reference':compatibility.images()[family],
            'image':{'Id':'sha256:'+'a'*64,'Architecture':'arm64','Os':'linux'},
            'compiler':{'path':'/synthetic/'+compiler+'/go','sha256':compiler[-2:]*32,
                'version':'go version go'+compiler+'.1 darwin/arm64'},
            'host':'synthetic','environment':dict(compatibility.ENVIRONMENT),'inputs':source,
            'required_tests':apps.required(),'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
            'inputs_stable':True,'exit_code':0,'restart_count':1,'resources_cleaned':True,
            'lost_prior_passes':[],'prior_profile':None,'stages':{},'logs':{},
            'server':{'endpoint':endpoint,'command':provenance.server_command(endpoint,'mariadb'),
                'before':server.copy(),'after':server.copy(),'stable':True}}
        for stage,argv in apps.commands(capture['compiler']['path']).items():
            events=[]
            for key in apps.REQUIRED.get(stage,()):
                package,_,test=key.partition('/Test')
                events.append({'Action':'pass','Package':package,'Test':'Test'+test})
            if stage=='applications':
                for test,output in (('TestApplicationProvenance','MARIADB_APPLICATION '+json.dumps({
                    'server_version':server['version'],'driver_module':apps.ci.PACKAGE,'registration':'mariadb'})),
                    ('TestApplicationRestart','APPLICATION_RESTART completed=1')):
                    events.append({'Action':'output','Package':apps.PACKAGE,'Test':test,'Output':output})
            output='\n'.join(map(json.dumps,events))+'\n'
            if stage=='modules':
                values=copy.deepcopy(json.loads((record.HERE/'apps/dependencies.json').read_text()))
                for value in values:
                    if 'local_source' in value:
                        module=value.pop('local_source');suffix='' if module==apps.ci.PACKAGE else 'integrations/gorm'
                        value['Replace']={'Dir':str(record.ROOT/suffix)}
                output='\n'.join(map(json.dumps,values))
            if stage=='consumer':
                output=json.dumps({'contract':'mariadb-consumer-v1','driver_files':source['driver'],
                    'core_go_mod_sha256':source['driver']['go.mod'],
                    'consumer_source_sha256':source['workload']['validation/apps/consumer/main.go'],
                    'cgo_enabled':False,'workspace':'off','network':'off','build':True,'vet':True,
                    'output':'MariaDB Alpha consumer ready','framework_dependencies_present':False,
                    'temporary_export_removed_on_exit':True})
            (directory/(stage+'.log')).write_text(output)
            issues,cases=apps.assess(output,0,stage)
            self.assertEqual(issues,[])
            capture['stages'][stage]={'command':argv,'exit_code':0,'issues':issues,'cases':cases}
        for phase in ('before','after'):(directory/f'server-{phase}.log').write_text(json.dumps(server))
        (directory/'server.log').write_text('owned fixture')
        self.save(capture,directory)
        return capture

    @staticmethod
    def save(capture,directory):
        capture['logs']={path.name:record.digest(path.read_bytes()) for path in directory.glob('*.log')}
        (directory/'capture.json').write_text(json.dumps(capture))

    def native(self,directory):
        capture=self.valid(directory)
        capture['native_contract']=native.CONTRACT
        capture['compiler']['version']='go version go1.27.1 linux/arm64'
        capture['native_fixture']={'contract':'cdc-v1','fixture_sha256':capture['inputs']['workload']['validation/cdc_fixture.py'],
            'transport':'private-unix-socket','server_version':capture['server']['before']['version']}
        capture['server']['endpoint']={'socket':'/private/owned/server.sock'}
        capture['server']['command']=native.socket_command(capture['server']['endpoint'],'mariadb')
        self.save(capture,directory)
        return capture

    def add_pass(self,capture,directory,name='TestLinuxApplicationRegression'):
        path=directory/'applications.log'
        path.write_text(path.read_text()+json.dumps({'Action':'pass','Package':apps.PACKAGE,'Test':name})+'\n')
        _,capture['stages']['applications']['cases']=apps.assess(path.read_text(),0,'applications')
        self.save(capture,directory)

    def test_native_history_preserves_extra_leaves_and_requires_latest(self):
        old_dir=record.HERE/'results/native-old';old_dir.mkdir()
        old=self.native(old_dir);self.add_pass(old,old_dir)
        new_dir=record.HERE/'results/native-new';new_dir.mkdir()
        current=self.native(new_dir);current['recorded_at']='2026-11-14T02:00:00+00:00'
        current['prior_profile']=native.prior_profile(current)
        self.assertEqual(current['prior_profile']['passing'],sorted(apps.passing(old)))
        with self.assertRaisesRegex(ValueError,'passes lost'):native.validate(current,new_dir)
        self.add_pass(current,new_dir)
        native.validate(current,new_dir)
        current['prior_profile']=None
        with self.assertRaisesRegex(ValueError,'predecessor was omitted'):native.validate(current,new_dir)
        current['prior_profile']=native.prior_profile(current)
        current['lost_prior_passes']=['declared lost']
        with self.assertRaisesRegex(ValueError,'passes lost'):native.validate(current,new_dir)


    def test_native_history_hash_profile_and_raw_evidence_fail_closed(self):
        old_dir=record.HERE/'results/native-old';old_dir.mkdir();old=self.native(old_dir)
        new_dir=record.HERE/'results/native-new';new_dir.mkdir();current=self.native(new_dir)
        current['recorded_at']='2026-11-14T02:00:00+00:00'
        current['prior_profile']=native.prior_profile(current)
        native.validate(current,new_dir)
        current['prior_profile']['profile']['host']='wrong'
        with self.assertRaisesRegex(ValueError,'snapshot differs'):native.validate(current,new_dir)
        current['prior_profile']=native.prior_profile(current)
        path=old_dir/'capture.json';raw=path.read_bytes();path.write_text('{}')
        with self.assertRaisesRegex(ValueError,'history source changed'):native.validate(current,new_dir)
        path.write_bytes(raw)
        path=old_dir/'adapter.log';path.write_text('changed')
        with self.assertRaisesRegex(ValueError,'path/hash'):native.prior_profile(current)

    def test_native_v1_is_historical_only_and_keeps_original_inventory(self):
        old_dir=record.HERE/'results/native-old';old_dir.mkdir();old=self.native(old_dir)
        old['native_contract']='mariadb-applications-native-v1'
        del old['prior_profile'];del old['lost_prior_passes']
        old['required_tests']=apps.frozen_contract()['required_tests']
        self.save(old,old_dir)
        native.validate(old,old_dir,current=False)
        with self.assertRaises(ValueError):native.validate(old,old_dir)
        new_dir=record.HERE/'results/native-new';new_dir.mkdir();current=self.native(new_dir)
        current['recorded_at']='2026-11-14T02:00:00+00:00'
        # Fixture source changes and random socket names must not reset history.
        current['native_fixture']['fixture_sha256']='new-script'
        current['server']['endpoint']['socket']='/different/owned/server.sock'
        self.assertEqual(native.profile(old),native.profile(current))
        self.assertEqual(native.prior_profile(current)['source'],str((old_dir/'capture.json').relative_to(record.HERE)))

    def test_native_timeout_keeps_partial_output_stage_status_and_failure(self):
        control=record.HERE/'owned';control.mkdir();(control/'server.log').write_text('owned')
        binary=control/'go';binary.write_text('compiler')
        directory=record.HERE/'results/timeout'
        env={'GO_MARIADB_CDC_CONTROL':str(control),'GO_MARIADB_CDC_SOCKET':str(control/'server.sock'),
             'GO_MARIADB_CDC_TLS_CA':str(control/'ca.pem'),'GO_MARIADB_CDC_RECEIPT':'{}'}
        with mock.patch.dict(os.environ,env), mock.patch.object(native.shutil,'which',side_effect=lambda name:str(binary) if name=='go' else '/usr/bin/mariadb'), \
             mock.patch.object(native.provenance,'revision_info',return_value={}), \
             mock.patch.object(native,'prior_profile',return_value=None), \
             mock.patch.object(native.subprocess,'check_output',side_effect=['go version go1.27.1 linux/arm64',json.dumps(sample_server())]), \
             mock.patch.object(native.subprocess,'run',side_effect=[subprocess.CompletedProcess([],0),subprocess.TimeoutExpired(['go'],300,output=b'partial-stage-marker\n')]):
            self.assertEqual(native.child('go',directory),1)
        capture=json.loads((directory/'capture.json').read_text())
        self.assertTrue(capture['stages']['applications']['timed_out'])
        self.assertIsNone(capture['stages']['applications']['exit_code'])
        self.assertEqual(capture['failure']['phase'],'check_applications')
        self.assertIn('partial-stage-marker',(directory/'applications.log').read_text())

    def test_native_streamed_output_survives_timeout_without_exception_output(self):
        def timeout(argv,**kwargs):
            kwargs['stdout'].write('streamed-marker\n')
            kwargs['stdout'].flush()
            raise subprocess.TimeoutExpired(argv,300)
        with mock.patch.object(native.subprocess,'run',side_effect=timeout):
            stage=native.run_stage('timeout',['go'],{},self.directory)
        self.assertTrue(stage['timed_out'])
        self.assertEqual((self.directory/'timeout.log').read_text(),'streamed-marker\n')

    def test_native_preflight_and_server_diagnostic_failures_still_record(self):
        directory=record.HERE/'results/preflight'
        with mock.patch.dict(os.environ,{},clear=True):
            self.assertEqual(native.child('missing-go',directory),1)
        capture=json.loads((directory/'capture.json').read_text())
        self.assertEqual(capture['failure'],{'phase':'fixture_preflight','error_type':'KeyError'})
        control=record.HERE/'missing-log';control.mkdir()
        directory=record.HERE/'results/diagnostic'
        with mock.patch.dict(os.environ,{'GO_MARIADB_CDC_CONTROL':str(control),'GO_MARIADB_CDC_SOCKET':'/outside/server.sock'}):
            self.assertEqual(native.child('go',directory),1)
        capture=json.loads((directory/'capture.json').read_text())
        self.assertEqual(capture['failure']['phase'],'fixture_preflight')
        self.assertEqual(capture['server_log_failure']['phase'],'server_diagnostics')

    def test_complete_profile_and_source_classification(self):
        apps.validate(self.capture,self.directory)
        source=record.inputs()
        self.assertIn('integrations/gorm/mariadb.go',source['driver'])
        self.assertIn('integrations/gorm/go.mod',source['driver'])
        self.assertIn('validation/apps/example/order.go',source['workload'])
        self.assertNotIn('validation/apps/example/order.go',source['driver'])
        self.assertIn('validation/apps/dependencies.json',source['workload'])
        self.assertIn('validation/applications-contract-v1.json',source['workload'])

    def test_required_work_cannot_skip_disappear_or_race(self):
        output=(self.directory/'applications.log').read_text()
        for variant in (output.replace('"pass"','"skip"',1),
                        '\n'.join(line for line in output.splitlines() if 'early_rows_close' not in line),
                        output+'\nWARNING: DATA RACE\n'):
            with self.subTest(output=variant[-50:]):self.assertTrue(apps.assess(variant,0,'applications')[0])

    def test_mutated_commands_source_cleanup_and_restart_fail(self):
        for key,value in (('inputs',{}),('resources_cleaned',False),('restart_count',True),
                          ('restart_count',0),('input_fingerprint','wrong'),('environment',{})):
            changed=copy.deepcopy(self.capture);changed[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):apps.validate(changed,self.directory)
        changed=copy.deepcopy(self.capture);changed['stages']['applications']['command']=['go','test','-bench=.']
        with self.assertRaises(ValueError):apps.validate(changed,self.directory)

    def test_provenance_markers_and_module_replacements_are_checked(self):
        for name,replace in (('applications',lambda s:s.replace('registration\\": \\"mariadb','registration\\": \\"mysql')),
                             ('modules',lambda s:s.replace(str(record.ROOT),'/unrelated/source')),
                             ('consumer',lambda s:s.replace('"build": true','"build": false'))):
            path=self.directory/(name+'.log');original=path.read_text();changed=replace(original)
            self.assertNotEqual(original,changed)
            path.write_text(changed);self.save(self.capture,self.directory)
            with self.subTest(stage=name),self.assertRaises(ValueError):apps.validate(self.capture,self.directory)
            path.write_text(original);self.save(self.capture,self.directory)
        path=self.directory/'applications.log';output=path.read_text()
        line=next(line for line in output.splitlines() if 'APPLICATION_RESTART' in line)
        for bad in (output+'\n'+line,output.replace('APPLICATION_RESTART','REMOVED')):
            path.write_text(bad);self.save(self.capture,self.directory)
            with self.assertRaises(ValueError):apps.validate(self.capture,self.directory)

    def test_log_hashes_and_symlink_escapes_fail(self):
        log=self.directory/'vet.log';log.write_text('changed')
        with self.assertRaisesRegex(ValueError,'path/hash'):apps.validate(self.capture,self.directory)
        outside=record.HERE/'private';outside.write_text('private')
        log.unlink();log.symlink_to(outside);self.save(self.capture,self.directory)
        with self.assertRaisesRegex(ValueError,'path/hash'):apps.validate(self.capture,self.directory)

    def test_historical_lock_and_commands_are_immutable(self):
        with mock.patch.object(apps,'commands',return_value={}):apps.validate(self.capture,self.directory,current=False)
        path=record.HERE/'applications-contract-v1.json';path.write_text('{}')
        with self.assertRaisesRegex(ValueError,'immutable'):apps.validate(self.capture,self.directory,current=False)

    def test_native_profiles_do_not_enter_docker_pass_history(self):
        directory=record.HERE/'results/native-prior';directory.mkdir()
        # Native receipts intentionally have no Docker image/family identity.
        (directory/'capture.json').write_text(json.dumps({'contract':apps.CONTRACT,
            'native_contract':native.CONTRACT,'recorded_at':'2026-11-13T00:00:00+00:00','exit_code':0}))
        self.assertIsNone(apps.prior_profile(self.capture))

    def test_prior_passing_leaf_cannot_be_lost_or_omitted(self):
        extra={'Action':'pass','Package':apps.PACKAGE,'Test':'TestFutureApplication'}
        path=self.directory/'applications.log';path.write_text(path.read_text()+json.dumps(extra)+'\n')
        _,self.capture['stages']['applications']['cases']=apps.assess(path.read_text(),0,'applications')
        self.save(self.capture,self.directory)
        directory=record.HERE/'results/new-name';directory.mkdir()
        current=self.valid(directory);current['recorded_at']='2026-11-14T02:00:00+00:00'
        self.save(current,directory)
        current['prior_profile']=apps.prior_profile(current)
        self.assertIsNotNone(current['prior_profile'])
        with self.assertRaisesRegex(ValueError,'passes lost'):apps.validate(current,directory)
        path=directory/'applications.log';path.write_text(path.read_text()+json.dumps(extra)+'\n')
        _,current['stages']['applications']['cases']=apps.assess(path.read_text(),0,'applications')
        self.save(current,directory);apps.validate(current,directory)
        current['prior_profile']=None
        with self.assertRaisesRegex(ValueError,'predecessor was omitted'):apps.validate(current,directory)

    def test_application_cohort_requires_all_cells_and_checks_summary(self):
        directories=[]
        for compiler in cohort.COMPILERS:
            for family in cohort.SERVERS:
                directory=record.HERE/'results'/f'{compiler}-{family}';directory.mkdir()
                self.valid(directory,compiler,family);directories.append(directory)
        # The ninth cell is a real independent native runtime contract, not a
        # relabelled Docker receipt. Supply its private socket provenance.
        directory=record.HERE/'results/native';directory.mkdir()
        self.native(directory);directories.append(directory)
        summary=cohort.publish(directories);self.assertEqual(cohort.verify_current(),summary)
        self.assertEqual(summary['profile_count'],9)
        self.assertIn('darwin/arm64 matrix clients',cohort.render(summary))
        for directory in directories[:-1]:
            capture=json.loads((directory/'capture.json').read_text())
            capture['compiler']['version']=capture['compiler']['version'].replace('darwin/','linux/')
            self.save(capture,directory)
        linux=cohort.build(directories)
        self.assertIn('linux/arm64 matrix clients',cohort.render(linux))
        self.assertNotIn('macOS',cohort.render(linux))
        for directory in directories[:4]:
            capture=json.loads((directory/'capture.json').read_text())
            capture['compiler']['version']=capture['compiler']['version'].replace('linux/','darwin/')
            self.save(capture,directory)
        mixed=cohort.build(directories)
        self.assertIn('darwin/arm64, linux/arm64 matrix clients',cohort.render(mixed))
        old_dir=record.HERE/'results/native-prior';old_dir.mkdir();old=self.native(old_dir)
        old['recorded_at']='2026-11-14T01:30:00+00:00'
        self.add_pass(old,old_dir)
        capture=json.loads((directories[-1]/'capture.json').read_text())
        capture['recorded_at']='2026-11-14T02:00:00+00:00'
        capture['prior_profile']=native.prior_profile(capture)
        self.save(capture,directories[-1])
        with self.assertRaisesRegex(ValueError,'passes lost'):cohort.build(directories)
        capture['native_contract']='unknown-native-contract'
        self.save(capture,directories[-1])
        with self.assertRaises(ValueError):cohort.build(directories)


        for collection in (directories[:-1],directories[:-1]+[directories[0]]):
            with self.assertRaises(ValueError):cohort.build(collection)
        for key,value in (('performance_measured',True),('profile_count',8),('artifacts',{})):
            changed=copy.deepcopy(summary);changed[key]=value
            with self.assertRaises(ValueError):cohort.validate(changed)


if __name__=='__main__':unittest.main()
