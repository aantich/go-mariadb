"""A known API gap must not hide absent fixtures, races or unrelated failures."""
import json
import unittest
import ci


def events(extra=()):
    records = []
    for name in ci.REQUIRED:
        package, _, test = name.rpartition('/Test')
        records.append({'Package':package, 'Test':'Test'+test, 'Action':'pass'})
    return '\n'.join(json.dumps(record) for record in [*records, *extra])


class CIPolicyTests(unittest.TestCase):
    def test_missing_fixture_suite_is_rejected(self):
        issues, _ = ci.assess_tests('', 0)
        self.assertTrue(issues)

    def test_required_successes_and_exact_known_gap_are_distinct(self):
        case = {'Package':ci.PACKAGE+'/validation', 'Test':'TestMariaDBDesiredCleanPoolReset'}
        output = events([{**case, 'Action':'output', 'Output':'GAP: pool checkout retains previous request user variables; ResetSession is liveness-only'},
                         {**case, 'Action':'fail'}, {'Package':ci.PACKAGE+'/validation', 'Action':'fail'}])
        issues, records = ci.assess_tests(output, 1)
        self.assertEqual(issues, [])
        self.assertEqual(records[ci.KNOWN_GAP]['status'], 'fail')

    def test_known_test_name_does_not_allow_an_unrelated_error(self):
        output = events([{'Package':ci.PACKAGE+'/validation', 'Test':'TestMariaDBDesiredCleanPoolReset', 'Action':'fail'}])
        self.assertTrue(ci.assess_tests(output, 1)[0])

    def test_skip_and_parent_cleanup_failure_are_rejected(self):
        for event in ({'Package':ci.PACKAGE+'/validation', 'Test':'TestMissingFixture', 'Action':'skip'},
                      {'Package':ci.PACKAGE+'/validation', 'Test':'TestParent', 'Action':'fail'}):
            self.assertTrue(ci.assess_tests(events([event]), 1)[0])

    def test_package_build_failure_and_race_are_rejected(self):
        self.assertTrue(ci.assess_tests(events([{'Package':ci.PACKAGE+'/broken', 'Action':'fail'}]), 1)[0])
        self.assertTrue(ci.assess_tests(events()+'\nWARNING: DATA RACE', 0)[0])
