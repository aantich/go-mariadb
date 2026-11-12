"""Regression checks for missing evidence and regenerated explanations."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from evidence import benchmark_issues
import record
import verify


class ReviewGuardTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.original = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
        cls.native = json.loads((record.NATIVE_BASELINE/'capture.json').read_text())
        # Use immutable references so these tests do not become stale when a
        # new live capture replaces current.json during ordinary development.
        cls.full = {**cls.original['benchmarks'], **cls.native['benchmarks']}

    def test_complete_original_and_native_references(self):
        self.assertEqual(benchmark_issues(self.full, [self.original, self.native]), [])

    def test_parent_pass_is_protected_without_counting_it_as_a_leaf(self):
        def event(test, action):
            return json.dumps({'Package':'github.com/aantich/go-mariadb','Test':test,'Action':action})
        output='\n'.join(event(name,'pass') for name in ('TestParent','TestParent/a/b'))
        all_cases=record.test_cases(output,include_containers=True)
        self.assertEqual(len(record.test_cases(output)),1)
        self.assertEqual(len(all_cases),2)
        reference={'stages':{name:{'cases':[]} for name in ('upstream','protocol','mariadb')}}
        reference['stages']['upstream']['cases']=[{'name':case['name'].replace('github.com/aantich/go-mariadb','github.com/go-sql-driver/mysql'),'status':'pass'} for case in all_cases]
        self.assertEqual(record.lost_reference_passes([reference],{'upstream':all_cases}),[])
        for name in ('TestParent','TestParent/a/b'):
            changed=record.test_cases(output+'\n'+event(name,'fail'),include_containers=True)
            self.assertEqual(record.lost_reference_passes([reference],{'upstream':changed}),['github.com/go-sql-driver/mysql/'+name])
        self.assertEqual(len(record.lost_reference_passes([reference],{})),2)

    def test_overview_distinguishes_historical_gap_from_managed_policy(self):
        current=copy.deepcopy(self.native)
        self.assertIn('assertion remains an API gap','\n'.join(record.overview(current,self.original)))
        for case in current['stages']['mariadb']['cases']:
            if case['name'].endswith('/TestMariaDBDesiredCleanPoolReset'):
                case['status']='pass'
        rendered='\n'.join(record.overview(current,self.original))
        self.assertNotIn('assertion remains an API gap',rendered)
        self.assertIn('ordinary pool checkout remains liveness-only',rendered)

    def test_entire_profile_cannot_disappear(self):
        for name in ('BenchmarkQuery', 'BenchmarkMariaDBNativeBulk/100/native_bulk'):
            current = dict(self.full)
            del current[name]
            self.assertIn('Missing benchmark profile: '+name, benchmark_issues(current, [self.original, self.native]))

    def test_seven_duplicate_or_wrong_family_samples_are_rejected(self):
        for mutation in ('duplicate', 'family'):
            current = copy.deepcopy(self.full)
            sample = current['BenchmarkQuery']['samples'][0]
            if mutation == 'duplicate':
                sample['repetition'] = current['BenchmarkQuery']['samples'][1]['repetition']
            else:
                sample['family'] = 'mariadb'
            self.assertTrue(benchmark_issues(current, [self.original, self.native]))

    def test_invalid_metrics_and_forged_median_are_rejected(self):
        for value in (float('nan'), float('inf'), -1, None):
            current = copy.deepcopy(self.full)
            current['BenchmarkQuery']['samples'][0]['ns/op'] = value
            self.assertTrue(benchmark_issues(current, [self.original, self.native]))
        current = copy.deepcopy(self.full)
        current['BenchmarkQuery']['median']['ns/op'] = 1
        self.assertTrue(benchmark_issues(current, [self.original, self.native]))

    def test_custom_reference_metrics_must_not_disappear(self):
        name = next(name for name,row in self.full.items() if set(row['median'])-{'ns/op','B/op','allocs/op','iterations','gomaxprocs'})
        metric = next(iter(set(self.full[name]['median'])-{'ns/op','B/op','allocs/op','iterations','gomaxprocs'}))
        current = copy.deepcopy(self.full)
        for sample in current[name]['samples']:
            del sample[metric]
        del current[name]['median'][metric]
        self.assertTrue(benchmark_issues(current, [self.original, self.native]))

    def test_generated_report_preserves_overview_and_workload_units(self):
        current = {**self.native, 'benchmarks': self.full}
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(record, 'HERE', Path(directory)):
            record.render(current, self.original)
            rendered = (Path(directory)/'RESULTS.md').read_text()
            self.assertIn('## Current stage at a glance', rendered)
            self.assertIn('Performance acceptance remains open', rendered)
            self.assertIn('1/16/100/1000 rows per transaction', rendered)
            self.assertIn('Inherited workloads retain their own row counts', rendered)
            self.assertIn('Prepared/native throughput ratio', rendered)

    def test_report_keeps_missing_reference_profile_visible(self):
        current = {**self.native, 'benchmarks': dict(self.full)}
        del current['benchmarks']['BenchmarkQuery']
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(record, 'HERE', Path(directory)):
            record.render(current, self.original)
            rendered = (Path(directory)/'RESULTS.md').read_text()
            self.assertRegex(rendered, r'`BenchmarkQuery`[^\n]+\*\*MISSING\*\*')

    def test_actual_verifier_rejects_an_entire_missing_profile(self):
        current = {**self.native, 'complete':True, 'inputs':record.inputs(),
                   'capabilities':json.loads((record.HERE/'capabilities.json').read_text()),
                   'benchmarks':dict(self.full)}
        del current['benchmarks']['BenchmarkQuery']
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'results').mkdir()
            (root/'results/current.json').write_text(json.dumps(current))
            (root/'baselines').symlink_to(record.HERE/'baselines', target_is_directory=True)
            (root/'capabilities.json').symlink_to(record.HERE/'capabilities.json')
            with mock.patch.object(verify, 'HERE', root), self.assertRaisesRegex(SystemExit, 'Missing benchmark profile: BenchmarkQuery'):
                verify.main()
