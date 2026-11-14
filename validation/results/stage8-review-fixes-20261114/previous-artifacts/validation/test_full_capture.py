"""Full measurement publication must enforce the same checks as collection.

These are synthetic receipts in temporary directories, never measured results.
They replay immutable samples solely to exercise the actual verifier paths.
"""
import copy
import gzip
import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest import mock
import subprocess

import ci
from contracts import FULL_CHECK_ENVIRONMENT, FULL_COMMANDS, measurement_commands
import full_capture
import provenance
import record
from test_provenance import sample_server
import verify
import pass_history
from stage4_workloads import STAGE4_PROFILES
from test_ci_policy import external_auth_receipt, external_auth_event, cdc_receipt, cdc_event


class FullCaptureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.original = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
        cls.native = json.loads((record.NATIVE_BASELINE/'capture.json').read_text())

    def valid_capture(self, directory):
        source = record.inputs()
        capture = {'validation_contract':full_capture.CONTRACT, 'check_environment':dict(FULL_CHECK_ENVIRONMENT),
                   'complete':True, 'exit_code':0, 'inputs_stable':True, 'inputs':source,
                   'input_fingerprint':record.digest(json.dumps(source,sort_keys=True).encode()),
                   'capabilities':json.loads((record.HERE/'capabilities.json').read_text()),
                   'external_auth':external_auth_receipt(),
                   'cdc_fixture':cdc_receipt(),
                   'execution_directory':str(directory.resolve()), 'lost_baseline_passes':[], 'logs':{},
                   'evidence_directory':'results/full-test',
                   'hardware':copy.deepcopy(self.native['hardware']),
                   **{key:copy.deepcopy(self.original[key]) for key in ('go','host','modules','server')}}
        capture['benchmarks'] = copy.deepcopy({**self.original['benchmarks'], **self.native['benchmarks']})
        if record.STAGE4_BASELINE.exists():
            capture['benchmarks'].update(json.loads((record.STAGE4_BASELINE/'capture.json').read_text())['benchmarks'])
        else:
            for name, units in STAGE4_PROFILES.items():
                metrics={'ns/op':100.,'B/op':0.,'allocs/op':0.,**units}
                capture['benchmarks'][name]={'samples':[{'family':'mariadb','repetition':r,'iterations':1,'gomaxprocs':4,**metrics} for r in range(7)],'median':metrics}
        identities = {name:set() for name in full_capture.ORDINARY}
        for reference in (self.original, self.native):
            for name in identities:
                identities[name].update(case['name'].replace('github.com/go-sql-driver/mysql',ci.PACKAGE)
                                        for case in reference['stages'][name]['cases'] if case.get('status') == 'pass')
        for name in ci.REQUIRED:
            identities['mariadb' if '/validation/' in name else 'upstream'].add(name)
        outputs = {}
        for name, cases in identities.items():
            events = [json.dumps(external_auth_event()),json.dumps(cdc_event())] if name == 'mariadb' else []
            for case in sorted(cases):
                package, test = re.split(r'/(?=Test|Fuzz|Example)',case,maxsplit=1)
                events.append(json.dumps({'Package':package, 'Test':test, 'Action':'pass'}))
            outputs[name] = '\n'.join(events)+'\n'
        outputs['race'] = '\n'.join(outputs[name] for name in full_capture.ORDINARY)
        capture['stages'] = {}
        for name, command in measurement_commands(directory.resolve()).items():
            capture['stages'][name] = {'command':list(command), 'exit_code':0}
            if name in (*full_capture.ORDINARY,'race'):
                capture['stages'][name]['cases'] = record.test_cases(outputs[name])
            elif name.startswith('bench-'):
                _, family, repetition = name.split('-')
                rows = []
                for profile, row in capture['benchmarks'].items():
                    for sample in row['samples']:
                        if sample['family'] == family and sample['repetition'] == int(repetition):
                            metrics = ' '.join(str(value)+' '+key for key,value in sample.items()
                                               if key not in ('family','repetition','gomaxprocs','iterations'))
                            rows.append(f"{profile}-4 {sample['iterations']} {metrics}")
                outputs[name] = '\n'.join(rows)+'\n'
                capture['stages'][name]['profiles'] = len(rows)
            else:
                outputs[name] = 'synthetic check fixture\n'
            self.write_log(capture,directory,name,outputs[name])
        snapshot = sample_server()
        for key in snapshot.keys() & capture['server'].keys():
            snapshot[key] = int(capture['server'][key]) if key in provenance.SERVER_NUMBERS else capture['server'][key]
        snapshot.update(character_set_server=capture['server']['charset'], collation_server=capture['server']['collation'])
        endpoint = {'host':'127.0.0.1','port':3307,'user':'gomdb_validation','database':'go_mariadb_validation'}
        capture['server_provenance'] = {'endpoint':endpoint, 'command':provenance.server_command(endpoint,'mariadb'),
                                        'before':copy.deepcopy(snapshot), 'after':copy.deepcopy(snapshot), 'stable':True}
        for phase in ('before','after'):
            self.write_server_log(capture,directory,phase,snapshot)
        path=directory/'prior-capture.json'
        path.write_text(json.dumps({'complete':True,'stages':{name:capture['stages'][name] for name in full_capture.ORDINARY}}))
        capture['logs'][path.name]=record.digest(path.read_bytes())
        pass_history.write_snapshot(capture,directory)
        return capture, outputs

    def write_log(self, capture, directory, name, output):
        path = directory/(name+'.log.gz')
        path.write_bytes(gzip.compress(output.encode(),mtime=0))
        capture['logs'][path.name] = record.digest(path.read_bytes())

    def write_server_log(self, capture, directory, phase, snapshot):
        path = directory/('server-'+phase+'.log')
        path.write_text(json.dumps(snapshot)+'\n')
        capture['logs'][path.name] = record.digest(path.read_bytes())

    def test_complete_full_receipt_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,_=self.valid_capture(root)
            self.assertTrue(full_capture.validate(capture,root)['race'])

    def test_check_commands_environment_and_stages_cannot_be_weakened(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,_=self.valid_capture(root)
            for name in FULL_COMMANDS:
                changed=copy.deepcopy(capture); changed['stages'][name]['command']=['true']
                with self.subTest(stage=name), self.assertRaisesRegex(ValueError,'command differs'):
                    full_capture.validate(changed,root)
            for flags in ({'GOFLAGS':'-overlay=/tmp/other.json'}, {'GOWORK':'/tmp/go.work'}, {'GOMAXPROCS':'8'}):
                changed=copy.deepcopy(capture); changed['check_environment'].update(flags)
                with self.assertRaisesRegex(ValueError,'environment'):
                    full_capture.validate(changed,root)
            changed=copy.deepcopy(capture); del changed['stages']['race']
            with self.assertRaisesRegex(ValueError,'stages'):
                full_capture.validate(changed,root)

    def test_required_new_fixture_skip_or_removal_fails_collection_and_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,outputs=self.valid_capture(root)
            for stage in ('mariadb','race'):
                for action in ('skip','remove'):
                    changed=copy.deepcopy(capture); changed_outputs=dict(outputs)
                    events=[json.loads(line) for line in outputs[stage].splitlines() if line]
                    for event in events:
                        if event['Test'] == 'TestMariaDBProgressExecution':
                            event['Action']=action
                    events=[event for event in events if event['Action'] != 'remove']
                    changed_outputs[stage]='\n'.join(json.dumps(event) for event in events)
                    changed['stages'][stage]['cases']=record.test_cases(changed_outputs[stage])
                    self.write_log(changed,root,stage,changed_outputs[stage])
                    with self.subTest(stage=stage,action=action):
                        with self.assertRaisesRegex(ValueError,'fixture|coverage'):
                            full_capture.validate_checks(changed,changed_outputs)
                        with self.assertRaisesRegex(ValueError,'fixture|coverage'):
                            full_capture.validate(changed,root)
                    self.write_log(capture,root,stage,outputs[stage])

    def test_raw_log_checksums_case_summaries_and_benchmark_samples_are_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,outputs=self.valid_capture(root)
            changed=copy.deepcopy(capture); changed['stages']['race']['cases']=[]
            with self.assertRaisesRegex(ValueError,'Raw full-run test events'):
                full_capture.validate(changed,root)
            (root/'vet.log.gz').write_bytes(b'corrupted')
            with self.assertRaisesRegex(ValueError,'checksum mismatch'):
                full_capture.validate(capture,root)
            self.write_log(capture,root,'vet',outputs['vet'])
            changed=copy.deepcopy(capture); name=next(iter(changed['benchmarks']))
            changed['benchmarks'][name]['samples'][0]['ns/op'] *= 2
            with self.assertRaisesRegex(ValueError,'Raw benchmark samples'):
                full_capture.validate(changed,root)
            del capture['logs']['server-after.log']
            with self.assertRaisesRegex(ValueError,'log set'):
                full_capture.validate(capture,root)

    def test_settings_drift_or_an_unrelated_comparison_server_cannot_publish(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,_=self.valid_capture(root)
            changed=copy.deepcopy(capture); changed['server_provenance']['after']['sql_mode']='ANSI'
            self.write_server_log(changed,root,'after',changed['server_provenance']['after'])
            with self.assertRaisesRegex(ValueError,'Server settings changed'):
                full_capture.validate(changed,root)
            self.write_server_log(capture,root,'after',capture['server_provenance']['after'])
            # Both raw snapshots agree, but the comparison metadata came from
            # another server. Matching endpoints alone cannot repair that claim.
            changed=copy.deepcopy(capture); changed['server']['max_allowed_packet']='65536'
            with self.assertRaisesRegex(ValueError,'comparison settings'):
                full_capture.validate(changed,root)
            changed=copy.deepcopy(capture)
            for phase in ('before','after'):
                changed['server_provenance'][phase]['max_allowed_packet']=65536
                self.write_server_log(changed,root,phase,changed['server_provenance'][phase])
            changed['server']=full_capture.profile_server(changed['server_provenance']['before'])
            with self.assertRaisesRegex(ValueError,'reference profile'):
                full_capture.validate(changed,root)

    def test_actual_fresh_verifier_rejects_substituted_race_command(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); evidence=root/'results/full-test'; evidence.mkdir(parents=True)
            capture,_=self.valid_capture(evidence)
            capture['stages']['race']={'command':['true'],'exit_code':0,'cases':[]}
            (root/'results/current.json').write_text(json.dumps(capture))
            (root/'baselines').symlink_to(record.HERE/'baselines',target_is_directory=True)
            (root/'capabilities.json').symlink_to(record.HERE/'capabilities.json')
            with mock.patch.object(verify,'HERE',root), self.assertRaisesRegex(SystemExit,'command differs.*race'):
                verify.main()

    def test_two_endpoint_queries_use_their_own_settings(self):
        configs=[{'host':'127.0.0.1','port':port,'user':'gomdb_validation','database':'go_mariadb_validation','password':'private'}
                 for port in (3306,3307)]
        def query(command, **kwargs):
            snapshot=sample_server()
            snapshot['max_allowed_packet']=65536 if '--port=3307' in command else 402653184
            return subprocess.CompletedProcess(command,0,json.dumps(snapshot),'')
        with mock.patch.object(provenance.subprocess,'run',side_effect=query) as run:
            snapshots=[provenance.read_server(config,{},provenance.server_command(config,'mariadb'))[0] for config in configs]
        self.assertNotEqual(full_capture.profile_server(snapshots[0]),full_capture.profile_server(snapshots[1]))
        self.assertEqual(run.call_count,2)
        self.assertTrue(all('--protocol=tcp' in call.args[0] and '--no-defaults' in call.args[0] for call in run.call_args_list))

    def test_new_api_subtest_cannot_disappear_after_its_first_capture(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); capture,_=self.valid_capture(root)
            path=root/'prior-capture.json'; previous=json.loads(path.read_text())
            name=ci.PACKAGE+'/validation/TestMariaDBProgressExecution/new_case'
            previous['stages']['mariadb']['cases'].append({'name':name,'status':'pass'})
            path.write_text(json.dumps(previous));capture['logs'][path.name]=record.digest(path.read_bytes())
            with self.assertRaisesRegex(ValueError,'Prior passing assertions lost.*new_case'):
                full_capture.validate(capture,root)
            path.write_text('{}')
            with self.assertRaisesRegex(ValueError,'checksum mismatch'):
                full_capture.validate(capture,root)

    def test_collector_controls_environment_and_rejects_skips_or_setting_drift(self):
        for fault in (None,'skip','server-drift'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root=Path(directory); fixture=root/'fixture'; fixture.mkdir()
                valid,outputs=self.valid_capture(fixture)
                (root/'baselines').symlink_to(record.HERE/'baselines',target_is_directory=True)
                (root/'capabilities.json').symlink_to(record.HERE/'capabilities.json')
                (root/'results').mkdir()
                prior=(fixture/'prior-capture.json').read_text()
                (root/'results/current.json').write_text(prior)
                config={**valid['server_provenance']['endpoint'],'upstream_database':'go_mariadb_upstream','password':'synthetic-fixture-password'}
                path=root/'config.json'; path.write_text(json.dumps(config))
                calls=[]
                def run(command, **kwargs):
                    calls.append(command)
                    self.assertEqual({key:kwargs['env'][key] for key in FULL_CHECK_ENVIRONMENT},FULL_CHECK_ENVIRONMENT)
                    if '--protocol=tcp' in command:
                        self.assertEqual(command,provenance.server_command(config,command[0]))
                        snapshot=copy.deepcopy(valid['server_provenance']['before'])
                        if fault=='server-drift' and sum('--protocol=tcp' in call for call in calls)==2:
                            snapshot['sql_mode']='ANSI'
                        output=json.dumps(snapshot)+'\n'
                    else:
                        name=next(name for name,argv in FULL_COMMANDS.items() if command==list(argv))
                        output=outputs[name]
                        if fault=='skip' and name=='mariadb':
                            output+='\n'+json.dumps({'Package':ci.PACKAGE+'/validation','Test':'TestMariaDBProgressExecution','Action':'skip'})
                    return subprocess.CompletedProcess(command,0,output,'')
                def check_output(command, **kwargs):
                    if command==['go','version']:
                        return valid['go']+'\n'
                    if command==['go','list','-m','all']:
                        return '\n'.join(valid['modules'])+'\n'
                    if command[0]=='sysctl':
                        return '\n'.join(valid['hardware'])+'\n'
                    if command==['git','rev-parse','HEAD']:
                        return 'a'*40+'\n'
                    if command in (['git','ls-files'],['git','status','--short']):
                        return ''
                    self.fail('Unexpected subprocess: '+repr(command))
                with mock.patch.object(record,'HERE',root), mock.patch.object(record,'inputs',return_value=valid['inputs']), \
                        mock.patch.object(record.platform,'platform',return_value=valid['host']), \
                        mock.patch.object(record.shutil,'which',return_value='/usr/bin/mariadb'), \
                        mock.patch.object(record.subprocess,'run',side_effect=run), \
                        mock.patch.object(record.subprocess,'check_output',side_effect=check_output), \
                        mock.patch.dict('os.environ',{'GOFLAGS':'-overlay=/tmp/untrusted.json','GOWORK':'/tmp/other.work',
                                                     'GO_MARIADB_REQUIRE_INSTRUMENTATION':'0', 'GO_MARIADB_AUTH_RECEIPT':json.dumps(valid['external_auth']),
                                                     'GO_MARIADB_CDC_RECEIPT':json.dumps(valid['cdc_fixture'])}), \
                        mock.patch('sys.argv',['record.py','--config',str(path),'--conditions','synthetic test','--correctness-only']), \
                        mock.patch('builtins.print'):
                    code=record.main()
                self.assertEqual(code,int(fault is not None))
                self.assertEqual(sum('--protocol=tcp' in command for command in calls),2)
                self.assertFalse(any('-bench' in str(command) or '-test.bench' in str(command) for command in calls))
                self.assertEqual((root/'results/current.json').read_text(),prior)
