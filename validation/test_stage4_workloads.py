"""Check workload completeness and safe profiling defaults without timings."""
import copy
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import full_capture
import profile_reads
import record
from stage4_workloads import STAGE4_PROFILES, STAGE4_WORKLOAD_INPUTS, stage4_issues
import test_full_capture


class Stage4WorkloadTests(unittest.TestCase):
    def test_catalog_names_and_units_are_explicit(self):
        self.assertEqual(len(STAGE4_PROFILES),62)
        self.assertTrue(all(name.startswith('BenchmarkMariaDBStage4') for name in STAGE4_PROFILES))
        self.assertEqual({next(iter(units)) for units in STAGE4_PROFILES.values()}, {'rows/op','payload-B/op','commands/op'})
        for path in STAGE4_WORKLOAD_INPUTS:
            self.assertIn(path,record.inputs()['workload'])

    def test_an_unmeasured_reference_cannot_be_frozen(self):
        with mock.patch('sys.argv',['record.py','--correctness-only','--freeze-stage4-baseline','--conditions','synthetic check']), \
                self.assertRaisesRegex(SystemExit,'measurement is disabled'):
            record.main()

    def test_full_verifier_requires_new_profiles_even_without_a_first_reference(self):
        fixture=test_full_capture.FullCaptureTests();fixture.setUpClass()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);capture,_=fixture.valid_capture(root)
            self.assertEqual(stage4_issues(capture['benchmarks']),[])
            name=next(iter(STAGE4_PROFILES));del capture['benchmarks'][name]
            # Reconcile raw log identities as well, so a completely absent
            # profile cannot hide behind self-consistent receipt/log omissions.
            import gzip
            for key in capture['stages']:
                if key.startswith('bench-mariadb-'):
                    text=gzip.decompress((root/(key+'.log.gz')).read_bytes()).decode()
                    text='\n'.join(line for line in text.splitlines() if not line.startswith(name+'-'))+'\n'
                    fixture.write_log(capture,root,key,text)
                    capture['stages'][key]['profiles']-=1
            with self.assertRaisesRegex(ValueError,'Missing Stage 4 benchmark profile'):
                full_capture.validate(capture,root)

    def test_wrong_family_or_units_cannot_publish(self):
        name=next(iter(STAGE4_PROFILES));units=STAGE4_PROFILES[name]
        base={key:{'samples':[{'family':'mariadb',**value} for _ in range(7)],'median':value} for key,value in STAGE4_PROFILES.items()}
        for field,value in (('family','upstream'),(next(iter(units)),999)):
            changed=copy.deepcopy(base);changed[name]['samples'][0][field]=value
            self.assertTrue(stage4_issues(changed))

    def test_profiling_is_dry_run_by_default_without_touching_credentials_or_database(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'not-created'
            with mock.patch('sys.argv',['profile_reads.py','--case','text-blob-borrowed','--output',str(output)]), \
                    mock.patch.object(profile_reads.subprocess,'run',side_effect=AssertionError('dry-run launched subprocess')), \
                    mock.patch.object(profile_reads,'read_server',side_effect=AssertionError('dry-run queried database')), \
                    mock.patch('sys.stdout',new_callable=io.StringIO) as stream:
                self.assertEqual(profile_reads.main(),0)
            receipt=json.loads(stream.getvalue())
            self.assertFalse(receipt['execute'])
            self.assertFalse(output.exists())
            self.assertFalse(receipt['accepted_performance_measurement'])

    def test_profiling_selects_exactly_one_original_workload(self):
        for case,(shape,borrowed) in profile_reads.CASES.items():
            profile,commands=profile_reads.commands(case,Path('/tmp/example-profile'))
            self.assertEqual(profile,f'BenchmarkMariaDBRead/text/{shape}/raw={borrowed}')
            self.assertIn('-test.run=^$',commands[1])
            self.assertIn('-test.bench=^BenchmarkMariaDBRead$/^text$/^'+shape+'$/^raw='+borrowed+'$',commands[1])
            self.assertIn('-test.memprofilerate=1',commands[1])
            self.assertNotIn('-test.bench=.',commands[1])
