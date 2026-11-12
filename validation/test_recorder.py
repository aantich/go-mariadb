"""Regression checks for evidence parsing, not mirrors of the driver code."""
import json
import unittest
from run import benchmark_rows, test_cases


class RecorderTests(unittest.TestCase):
    def test_leaf_counts_do_not_double_count_parent_failures(self):
        events=[{'Package':'p','Test':name,'Action':status} for name,status in
                [('TestA','fail'),('TestA/child','fail'),('TestB','pass'),('TestC','skip')]]
        cases=test_cases('\n'.join(map(json.dumps,events)))
        self.assertEqual([(c['name'],c['status']) for c in cases],
                         [('p/TestA/child','fail'),('p/TestB','pass'),('p/TestC','skip')])

    def test_crashed_test_is_not_invented_as_a_pass(self):
        cases=test_cases(json.dumps({'Package':'p','Test':'TestCrash','Action':'run'}))
        self.assertNotIn('status',cases[0])

    def test_benchmark_metrics_keep_distinct_units(self):
        rows=benchmark_rows('BenchmarkRead/wide-4 100 123.5 ns/op 12.35 ns/row 44 B/op 2 allocs/op\n')
        self.assertEqual(rows['BenchmarkRead/wide']['ns/op'],123.5)
        self.assertEqual(rows['BenchmarkRead/wide']['ns/row'],12.35)
        self.assertEqual(rows['BenchmarkRead/wide']['B/op'],44)

    def test_failure_output_is_not_a_benchmark(self):
        self.assertEqual(benchmark_rows('--- FAIL: BenchmarkBroken (0.1s)\nFAIL'),{})


if __name__=='__main__':unittest.main()
