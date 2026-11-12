"""The optional native provider and real auth services must remain attributable."""
import copy
import gzip
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import auth_fixture
import ci
import external_auth_evidence
import record
import correctness
import full_capture
from test_ci_policy import external_auth_receipt, cdc_receipt


class ExternalAuthTests(unittest.TestCase):
    def fixture(self):
        source=record.inputs()
        receipt={'contract':'external-auth-v1','runtime_identity':'docker-runtime-sha256:'+'a'*64,
                 'server_version':'12.3.3-MariaDB','realm':'GO-MARIADB.TEST',
                 'service_principal':'mariadb/localhost@GO-MARIADB.TEST','tls_certificate_sha256':'b'*64,
                 'fixture_inputs':{name:source['workload']['validation/auth_fixture/'+name]
                                   for name in ('Dockerfile','serve.py','pam_fixture.c')}}
        event=json.dumps({'Test':'TestMariaDBExternalAuthentication','Output':'file.go:1: EXTERNAL_AUTH '+json.dumps(receipt)+'\n'})
        return {'external_auth':receipt,'inputs':source},event

    def test_native_and_service_inputs_are_fingerprinted(self):
        source=record.inputs()
        for name in ('gssapi/native.c','gssapi/native.h'):
            self.assertIn(name,source['driver'])
        for name in ('Dockerfile','Dockerfile.linux','serve.py','pam_fixture.c'):
            self.assertIn('validation/auth_fixture/'+name,source['workload'])

    def test_external_receipt_matches_raw_events(self):
        capture,output=self.fixture()
        external_auth_evidence.validate(capture,output)
        for changed,log in ((capture,''), ({'inputs':capture['inputs']},output)):
            with self.assertRaises(ValueError): external_auth_evidence.validate(changed,log)

    def test_current_contract_never_infers_historical_exemption_from_absence(self):
        capture, output = self.fixture()
        for changed, log in (({'inputs':capture['inputs']}, ''),
                             (capture, output+'\n'+output),
                             ({'method':'historical', 'validation_contract':'full-v1'}, '')):
            with self.assertRaises(ValueError):
                external_auth_evidence.validate(changed, log)

    def test_publication_rejects_omitted_metadata_event_or_both(self):
        # Reconcile the changed raw log and case output exactly as a collector
        # would. Checksums and all passing identities still agree: provenance
        # itself, rather than an incidental checksum error, must reject the run.
        from test_session_evidence import SessionEvidenceTests
        from test_full_capture import FullCaptureTests
        FullCaptureTests.setUpClass()
        for family in ('correctness', 'full'):
            for omission in ('metadata', 'event', 'both'):
                with self.subTest(family=family, omission=omission), tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    if family == 'correctness':
                        capture = SessionEvidenceTests().valid_capture(root)
                        correctness.validate(capture, root)
                        names = ('correctness',)
                    else:
                        capture, _ = FullCaptureTests().valid_capture(root)
                        full_capture.validate(capture, root)
                        names = ('mariadb', 'race')
                    if omission in ('metadata', 'both'):
                        del capture['external_auth']
                    if omission in ('event', 'both'):
                        for name in names:
                            path = root/(name+'.log'+('.gz' if family == 'full' else ''))
                            output = gzip.decompress(path.read_bytes()).decode() if family == 'full' else path.read_text()
                            output = '\n'.join(line for line in output.splitlines() if 'EXTERNAL_AUTH ' not in line)+'\n'
                            if family == 'full':
                                path.write_bytes(gzip.compress(output.encode(), mtime=0))
                                cases = record.test_cases(output)
                            else:
                                path.write_text(output)
                                issues, cases = ci.assess_tests(output, 0)
                                self.assertEqual(issues, [])
                            capture['logs'][path.name] = record.digest(path.read_bytes())
                            capture['stages'][name]['cases'] = cases
                    with self.assertRaisesRegex(ValueError, '(?i)external authentication'):
                        if family == 'correctness':
                            correctness.summarize(capture, root)
                        else:
                            full_capture.validate(capture, root)

    def test_external_receipt_rejects_wrong_source_or_runtime(self):
        capture,_=self.fixture()
        for field,value in (('runtime_identity','unattributed-tag'),('realm','OTHER.TEST'),
                            ('fixture_inputs',{'serve.py':'changed'})):
            changed=copy.deepcopy(capture);changed['external_auth'][field]=value
            output=json.dumps({'Test':'TestMariaDBExternalAuthentication','Output':'EXTERNAL_AUTH '+json.dumps(changed['external_auth'])})
            with self.assertRaises(ValueError): external_auth_evidence.validate(changed,output)

    def test_fixture_cleanup_requires_ownership_label(self):
        result=mock.Mock(returncode=0,stdout='other-owner\n')
        with mock.patch.object(auth_fixture.subprocess,'run',return_value=result) as run:
            auth_fixture.cleanup_container('go-mariadb-auth-unique')
        self.assertEqual(run.call_count,1)

    def test_auth_skip_or_absent_native_cleanup_is_not_accepted(self):
        from test_ci_policy import events
        output=events([{'Package':ci.PACKAGE+'/gssapi','Test':'TestNativeContextCleanup','Action':'skip'}])
        self.assertTrue(ci.assess_tests(output,0)[0])

    def test_lost_prior_pass_keeps_failed_capture_instead_of_crashing(self):
        from test_ci_policy import events
        from test_provenance import sample_server
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);config=root/'config.json';target=root/'evidence'
            config.write_text(json.dumps({'database':'go_mariadb_validation','upstream_database':'go_mariadb_upstream',
                'password':'synthetic-fixture-password','host':'127.0.0.1','port':3306,'user':'gomdb_validation'}))
            server=sample_server()
            def run(command,**kwargs):
                output=events() if '-json' in command else 'success\n'
                return subprocess.CompletedProcess(command,0,output,'')
            with mock.patch.dict('os.environ',{'MARIADB_VALIDATION_CONFIG':str(config),
                    'GO_MARIADB_AUTH_RECEIPT':json.dumps(external_auth_receipt()),
                    'GO_MARIADB_CDC_RECEIPT':json.dumps(cdc_receipt())},clear=True), \
                 mock.patch.object(ci.shutil,'which',return_value='mariadb'), \
                 mock.patch.object(ci,'read_server',return_value=(server,json.dumps(server))), \
                 mock.patch.object(ci,'revision_info',return_value={'commit':None}), \
                 mock.patch.object(ci.platform,'platform',return_value='synthetic host'), \
                 mock.patch.object(ci.subprocess,'check_output',return_value='synthetic compiler'), \
                 mock.patch.object(ci.subprocess,'run',side_effect=run), \
                 mock.patch.object(ci.pass_history,'preserve'), \
                 mock.patch.object(ci.pass_history,'lost',return_value=['p/TestPriorPassingLeaf']), \
                 mock.patch('sys.argv',['ci.py','--correctness-only','--output',str(target)]), \
                 mock.patch('builtins.print'):
                self.assertEqual(ci.main(),1)
            capture=json.loads((target/'capture.json').read_text())
            self.assertEqual(capture['exit_code'],1)
            self.assertEqual(capture['lost_correctness_passes'],['p/TestPriorPassingLeaf'])
            self.assertEqual(len(capture['logs']),5)
