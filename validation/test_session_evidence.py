"""Guard the explicit isolation contract and separate correctness provenance."""
import json
import copy
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import ci
import correctness
import prepare_sessions
import record
from contracts import CHECK_ENVIRONMENT, CORRECTNESS_COMMANDS
import provenance
import pass_history
from test_ci_policy import events
from test_provenance import sample_server


class SessionEvidenceTests(unittest.TestCase):
    def valid_capture(self, directory):
        output=events()
        issues, cases=ci.assess_tests(output,0)
        self.assertEqual(issues,[])
        commands={name:list(argv) for name,argv in CORRECTNESS_COMMANDS.items()}
        logs={}
        for name in commands:
            path=directory/(name+'.log')
            path.write_text(output if name=='correctness' else 'success\n')
            logs[path.name]=record.digest(path.read_bytes())
        server = sample_server()
        endpoint = {'host':'127.0.0.1','port':3306,'user':'gomdb_validation','database':'go_mariadb_validation'}
        for phase in ('before','after'):
            path=directory/('server-'+phase+'.log')
            path.write_text(json.dumps(server)+'\n')
            logs[path.name]=record.digest(path.read_bytes())
        source=record.inputs()
        capture = {'exit_code':0,'inputs_stable':True,'inputs':source,'go':'synthetic compiler','host':'synthetic host',
                'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
                'method':'correctness only, no benchmarks','capabilities':json.loads((record.HERE/'capabilities.json').read_text()),
                'check_environment':dict(CHECK_ENVIRONMENT),
                'server':{'endpoint':endpoint,'command':provenance.server_command(endpoint,'mariadb'),'before':server.copy(),'after':server.copy(),'stable':True},
                'stages':{name:{'command':command,'exit_code':0,'issues':[],**({'cases':cases} if name=='correctness' else {})} for name,command in commands.items()},'logs':logs}
        pass_history.write_snapshot(capture,directory)
        return capture

    def test_complete_receipt_passes_and_rejects_benchmark_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);capture=self.valid_capture(root)
            self.assertTrue(correctness.validate(capture,root))
            for command in (['go','test','-bench=.'],['go','test','-benchtime=1x']):
                changed=copy.deepcopy(capture);changed['stages']['vet']['command']=command
                with self.assertRaisesRegex(ValueError,'benchmark command'):
                    correctness.validate(changed,root)

    def test_raw_log_and_case_forgery_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);capture=self.valid_capture(root)
            changed=copy.deepcopy(capture);changed['stages']['correctness']['cases'].clear()
            with self.assertRaisesRegex(ValueError,'Raw correctness events'):
                correctness.validate(changed,root)
            (root/'correctness.log').write_text('overwritten\n')
            with self.assertRaisesRegex(ValueError,'checksum mismatch'):
                correctness.validate(capture,root)

    def test_missing_logs_and_tampered_fingerprint_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);capture=self.valid_capture(root)
            capture['input_fingerprint']='forged'
            with self.assertRaisesRegex(ValueError,'source fingerprint'):
                correctness.validate(capture,root)
            capture=self.valid_capture(root);del capture['logs']['vet.log']
            with self.assertRaisesRegex(ValueError,'log set is incomplete'):
                correctness.validate(capture,root)

    def test_slash_names_exclude_all_ancestors(self):
        records = {'p/Test':{'status':'pass'}, 'p/Test/a/b':{'status':'pass'}}
        self.assertEqual(list(correctness.leaves(records)), ['p/Test/a/b'])
        output = '\n'.join(json.dumps({'Package':'p','Test':name,'Action':'pass'}) for name in ('Test','Test/a/b'))
        self.assertEqual([case['name'] for case in record.test_cases(output)], ['p/Test/a/b'])

    def test_contract_migration_preserves_all_original_bytes(self):
        original = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
        self.assertTrue(record.baseline_workloads_match(original))
        changes = json.loads((record.HERE/'contract-migrations.json').read_text())['changes']
        self.assertEqual([change['path'] for change in changes], ['validation/suite_test.go'])
        self.assertIn('WithSessionReset',changes[0]['after'])

    def test_instrumentation_skip_is_profile_specific(self):
        case = {'Package':ci.PACKAGE+'/validation','Test':'TestMariaDBSessionInstrumentation'}
        output = events([{**case,'Action':'output','Output':'PROFILE: performance_schema requires the isolated instrumentation fixture'}, {**case,'Action':'skip'}])
        self.assertEqual(ci.assess_tests(output,0)[0],[])
        self.assertTrue(ci.assess_tests(output,0,require_instrumentation=True)[0])

    def test_failed_or_smoke_capture_cannot_publish_correctness(self):
        for capture in ({'exit_code':1}, {'exit_code':0,'inputs_stable':True,'inputs':record.inputs(),'method':'benchmark smoke'}):
            with self.assertRaises(ValueError):
                correctness.validate(capture,Path('.'))

    def test_missing_log_or_stage_cannot_publish(self):
        capture = {'exit_code':0,'inputs_stable':True,'inputs':record.inputs(),'method':'correctness only, no benchmarks','stages':{}}
        with self.assertRaises(ValueError):
            correctness.validate(capture,Path('.'))

    def test_instrumentation_provisioning_rejects_shared_server(self):
        config = {'database':'go_mariadb_validation','upstream_database':'go_mariadb_upstream','password':'a'*48}
        commands=[]
        with mock.patch.dict('os.environ',{},clear=True), self.assertRaises(ValueError):
            prepare_sessions.provision(config,commands.append,isolated=True)
        self.assertEqual(commands,[])

    def test_shared_provisioning_never_updates_instrumentation(self):
        config = {'database':'go_mariadb_validation','upstream_database':'go_mariadb_upstream','password':'a'*48}
        commands=[]
        prepare_sessions.provision(config,commands.append)
        self.assertFalse(any('performance_schema' in command or 'GLOBAL' in command for command in commands))

    def test_report_separates_correctness_and_old_timings(self):
        current=json.loads((record.HERE/'results/current.json').read_text())
        baseline=json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
        with tempfile.TemporaryDirectory() as directory:
            latest={**self.valid_capture(Path(directory)), 'recorded_at':'later','input_fingerprint':'new-source',
                    'leaf_counts':{'pass':1},'evidence_directory':'results/new','go':'go version test','host':'test host'}
            with mock.patch.object(record,'HERE',Path(directory)):
                record.render(current,baseline,latest)
                result=(Path(directory)/'RESULTS.md').read_text()
        self.assertIn('new-source',result)
        self.assertIn('No performance measurements were made for this source',result)
        self.assertIn('Earlier full-capture failures',result)
        self.assertIn('12.3.3-MariaDB',result)
        self.assertIn('test host',result)

    def test_required_commands_cannot_be_weakened_or_substituted(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture=self.valid_capture(root)
            changes=[('correctness',[arg for arg in CORRECTNESS_COMMANDS['correctness'] if arg!='-race']),
                     ('correctness',[arg for arg in CORRECTNESS_COMMANDS['correctness'] if not arg.startswith('-tags=')]),
                     ('correctness',[*CORRECTNESS_COMMANDS['correctness'],'-run=TestSubset']),
                     ('correctness',[*CORRECTNESS_COMMANDS['correctness'][:-1],'.']),
                     ('recorder',['python3','-c','pass']),('vet',['go','version'])]
            for name,command in changes:
                changed=copy.deepcopy(capture); changed['stages'][name]['command']=command
                with self.subTest(name=name,command=command), self.assertRaisesRegex(ValueError,'Required check command'):
                    correctness.validate(changed,root)
            changed=copy.deepcopy(capture); changed['check_environment']['GOFLAGS']='-run=TestSubset'
            with self.assertRaisesRegex(ValueError,'check environment'):
                correctness.validate(changed,root)

    def test_missing_or_changed_server_provenance_cannot_publish(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture=self.valid_capture(root)
            changes=[{}, {**capture['server'],'after':{**capture['server']['after'],'sql_mode':'ANSI'}},
                     {**capture['server'],'stable':False}]
            for server in changes:
                changed=copy.deepcopy(capture); changed['server']=server
                with self.assertRaises(ValueError):
                    correctness.validate(changed,root)
            changed=copy.deepcopy(capture); changed['instrumentation_required']=True
            changed['server']['before']['performance_schema']=0
            changed['server']['after']['performance_schema']=0
            for phase in ('before','after'):
                path=root/('server-'+phase+'.log'); path.write_text(json.dumps(changed['server'][phase]))
                changed['logs'][path.name]=record.digest(path.read_bytes())
            with self.assertRaisesRegex(ValueError,'instrumentation is disabled'):
                correctness.validate(changed,root)

    def test_renderer_cannot_claim_substituted_checks_passed(self):
        with tempfile.TemporaryDirectory() as directory:
            capture=self.valid_capture(Path(directory)); capture['stages']['vet']['command']=['go','version']
            with self.assertRaisesRegex(ValueError,'Required check command'):
                record.render({}, {}, capture)

    def test_matching_raw_snapshots_still_reject_server_setting_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture=self.valid_capture(root)
            capture['server']['after']['sql_mode']='ANSI'
            path=root/'server-after.log'
            path.write_text(json.dumps(capture['server']['after']))
            capture['logs'][path.name]=record.digest(path.read_bytes())
            with self.assertRaisesRegex(ValueError,'Server settings changed'):
                correctness.validate(capture,root)
