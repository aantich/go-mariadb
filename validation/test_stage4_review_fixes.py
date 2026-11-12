"""Exercise the actual history/publishing and failure paths without measurements."""
import copy
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import ci
import correctness
import full_capture
import pass_history
import profile_reads
import record
import test_full_capture
import test_session_evidence
from contracts import FULL_COMMANDS


class CorrectnessHistoryTests(unittest.TestCase):
    def fixture(self, directory, extra=False):
        directory.mkdir(parents=True)
        capture = test_session_evidence.SessionEvidenceTests().valid_capture(directory)
        capture['recorded_at'] = '2026-11-12T17:00:00+00:00'
        if extra:
            case = {'Package':ci.PACKAGE,'Test':'TestNativeStatementLifetime/retained_case','Action':'pass'}
            path = directory/'correctness.log'
            path.write_text(path.read_text()+'\n'+json.dumps(case)+'\n')
            _, capture['stages']['correctness']['cases'] = ci.assess_tests(path.read_text(),0)
            capture['logs'][path.name] = record.digest(path.read_bytes())
        (directory/'capture.json').write_text(json.dumps(capture))
        return capture

    def test_correctness_publisher_rejects_a_lost_newer_leaf_before_updating_pointers(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            previous=self.fixture(root/'results/history',extra=True)
            current=self.fixture(root/'results/attempt')
            (root/'capabilities.json').symlink_to(record.HERE/'capabilities.json')
            pointer=root/'results/correctness-current.json'
            pointer.write_text(json.dumps({**previous,'evidence_directory':'results/history'}))
            saved=pointer.read_bytes()
            pass_history.preserve(current,root/'results/attempt',root)
            with mock.patch.object(record,'HERE',root), mock.patch.object(record,'inputs',return_value=current['inputs']), mock.patch.object(record,'render') as render:
                with self.assertRaisesRegex(ValueError,'Prior correctness passing leaves lost.*retained_case'):
                    correctness.publish(current,root/'results/attempt')
                render.assert_not_called()
            self.assertEqual(pointer.read_bytes(),saved)

    def test_snapshot_survives_pointer_advancement_and_detects_skip_or_omission(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); previous=self.fixture(root/'results/history',extra=True)
            current=self.fixture(root/'results/attempt')
            pass_history.preserve(current,root/'results/attempt',root)
            observed=pass_history.statuses(previous)
            pass_history.validate(current,root/'results/attempt',observed)
            name=record.case_key(ci.PACKAGE+'/TestNativeStatementLifetime/retained_case')
            for status in (None,'skip','fail'):
                changed=dict(observed)
                if status is None: del changed[name]
                else: changed[name]=status
                self.assertEqual(pass_history.lost(current,root/'results/attempt',changed),[name])
            (root/'results/history/capture.json').unlink()
            pass_history.validate(current,root/'results/attempt',observed)

    def test_profiles_are_separate_and_corrupt_matching_history_is_not_silently_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); previous=self.fixture(root/'results/history',extra=True)
            current=self.fixture(root/'results/attempt')
            other=copy.deepcopy(current);other['host']='different operating system'
            pass_history.preserve(other,root/'results/attempt',root)
            self.assertIsNone(json.loads((root/'results/attempt/prior-correctness.json').read_text())['reference'])
            (root/'results/history/vet.log').write_text('changed')
            with self.assertRaisesRegex(ValueError,'Prior correctness log checksum'):
                pass_history.preserve(current,root/'results/attempt',root)

    def test_missing_or_modified_snapshot_and_wrong_profile_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture=self.fixture(root/'results/attempt')
            target=root/'results/attempt'
            changed=copy.deepcopy(capture);del changed['pass_history_contract']
            with self.assertRaisesRegex(ValueError,'history contract'):
                pass_history.validate(changed,target,{})
            changed=copy.deepcopy(capture);changed['host']='other host'
            with self.assertRaisesRegex(ValueError,'profile differs'):
                pass_history.validate(changed,target,{})
            (target/pass_history.FILENAME).write_text('{}')
            with self.assertRaisesRegex(ValueError,'snapshot checksum'):
                pass_history.validate(capture,target,{})

    def test_full_measurement_verifier_protects_correctness_only_passes(self):
        fixture=test_full_capture.FullCaptureTests();fixture.setUpClass()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);capture,_=fixture.valid_capture(root)
            previous=self.fixture(root/'ci-reference',extra=True)
            previous['go'],previous['host']=capture['go'],capture['host']
            for phase in ('before','after'):
                previous['server'][phase]=capture['server_provenance'][phase]
            pass_history.write_snapshot(capture,root,previous,'synthetic correctness receipt')
            with self.assertRaisesRegex(ValueError,'Prior correctness passing leaves lost.*retained_case'):
                full_capture.validate(capture,root)

    def test_collector_rejects_an_omitted_correctness_only_leaf(self):
        # Exercise record.main without launching any check or timing process.
        fixture=test_full_capture.FullCaptureTests();fixture.setUpClass()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);work=root/'fixture';work.mkdir()
            valid,outputs=fixture.valid_capture(work)
            reference=self.fixture(root/'reference',extra=True)
            reference['go'],reference['host']=valid['go'],valid['host']
            for phase in ('before','after'):
                reference['server'][phase]=valid['server_provenance'][phase]
            (root/'results').mkdir()
            (root/'results/current.json').write_text((work/'prior-capture.json').read_text())
            (root/'baselines').symlink_to(record.HERE/'baselines',target_is_directory=True)
            (root/'capabilities.json').symlink_to(record.HERE/'capabilities.json')
            config={**valid['server_provenance']['endpoint'],'upstream_database':'go_mariadb_upstream','password':'private'}
            cfg=root/'config.json';cfg.write_text(json.dumps(config))
            def run(command,**kwargs):
                if '--protocol=tcp' in command: output=json.dumps(valid['server_provenance']['before'])+'\n'
                else:
                    name=next(name for name,args in FULL_COMMANDS.items() if command==list(args))
                    output=outputs[name]
                return subprocess.CompletedProcess(command,0,output,'')
            def check_output(command,**kwargs):
                if command==['go','version']: return valid['go']+'\n'
                if command==['go','list','-m','all']: return '\n'.join(valid['modules'])+'\n'
                if command[0]=='sysctl': return '\n'.join(valid['hardware'])+'\n'
                if command==['git','rev-parse','HEAD']: return 'a'*40+'\n'
                if command in (['git','ls-files'],['git','status','--short']): return ''
                self.fail('Unexpected subprocess '+repr(command))
            def preserve(capture,path,here): pass_history.write_snapshot(capture,path,reference,'synthetic correctness history')
            with mock.patch.object(record,'HERE',root), mock.patch.object(record,'inputs',return_value=valid['inputs']), \
                 mock.patch.object(record.platform,'platform',return_value=valid['host']), \
                 mock.patch.object(record.shutil,'which',return_value='/synthetic/mariadb'), \
                 mock.patch.object(record.subprocess,'run',side_effect=run), \
                 mock.patch.object(record.subprocess,'check_output',side_effect=check_output), \
                 mock.patch.object(pass_history,'preserve',side_effect=preserve), \
                 mock.patch.dict('os.environ',{'GO_MARIADB_REQUIRE_INSTRUMENTATION':'0'}), \
                 mock.patch('sys.argv',['record.py','--config',str(cfg),'--conditions','synthetic','--correctness-only']), \
                 mock.patch('builtins.print'):
                self.assertEqual(record.main(),1)
            receipt=json.loads(next((root/'results/runs').glob('*/capture.json')).read_text())
            self.assertTrue(any('retained_case' in name for name in receipt['lost_correctness_passes']))


class DiagnosticFailureTests(unittest.TestCase):
    def test_failures_retain_attributable_redacted_receipts_without_running_commands(self):
        for fault in ('before','build_timeout','build_oserror','profile_timeout','after'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root=Path(directory);output=root/'diagnostic';config=root/'config.json'
                secret='private-placeholder-do-not-record'
                config.write_text(json.dumps({'database':'go_mariadb_validation','upstream_database':'go_mariadb_upstream',
                    'host':'127.0.0.1','port':3306,'user':'synthetic','password':secret}))
                def run(command,**kwargs):
                    is_profile=command[0].endswith('validation.test')
                    if fault=='build_oserror' and not is_profile: raise OSError(secret)
                    if (fault=='build_timeout' and not is_profile) or (fault=='profile_timeout' and is_profile):
                        raise subprocess.TimeoutExpired(command,180,output=b'partial '+secret.encode(),stderr=b'partial stderr')
                    for arg in command:
                        if arg.startswith(('-test.cpuprofile=','-test.memprofile=')):
                            Path(arg.split('=',1)[1]).write_bytes(b'synthetic profile, never measured')
                    return subprocess.CompletedProcess(command,0,stdout='',stderr='')
                snapshots=[ValueError(secret)] if fault=='before' else [({'version':'synthetic'},'synthetic server provenance\n'),
                    ValueError(secret) if fault=='after' else ({'version':'synthetic'},'synthetic server provenance\n')]
                argv=['profile_reads.py','--case','text-narrow-owned','--config',str(config),'--output',str(output),
                      '--execute','--conditions','synthetic policy test; no process launched']
                with mock.patch('sys.argv',argv), mock.patch.object(profile_reads.shutil,'which',return_value='/synthetic/mariadb'), \
                     mock.patch.object(profile_reads,'read_server',side_effect=snapshots), \
                     mock.patch.object(profile_reads.subprocess,'run',side_effect=run), mock.patch('sys.stdout',new_callable=io.StringIO):
                    self.assertEqual(profile_reads.main(),1)
                capture=json.loads((output/'capture.json').read_text())
                self.assertEqual(capture['status'],'failed')
                self.assertEqual(capture['exit_code'],1)
                self.assertFalse(capture['accepted_performance_measurement'])
                self.assertIn('input_fingerprint',capture)
                for name,sha in capture['artifacts'].items():
                    self.assertEqual(record.digest((output/name).read_bytes()),sha)
                for path in output.iterdir():
                    self.assertNotIn(secret.encode(),path.read_bytes())
                if fault=='after':
                    self.assertIn('cpu.pprof',capture['artifacts'])
                    self.assertIn('heap.pprof',capture['artifacts'])
                if fault.endswith('timeout'):
                    self.assertIn('[redacted]',(output/('profile.log' if fault=='profile_timeout' else 'build.log')).read_text())
