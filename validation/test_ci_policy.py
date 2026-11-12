"""A known API gap must not hide absent fixtures, races or unrelated failures."""
import json
import unittest
import ci
import record


def external_auth_receipt():
    """Explicit synthetic provenance for verifier tests; never a measured receipt."""
    source = record.inputs()
    return {'contract':'external-auth-v1', 'runtime_identity':'docker-runtime-sha256:'+'a'*64,
            'server_version':'12.3.3-MariaDB', 'realm':'GO-MARIADB.TEST',
            'service_principal':'mariadb/localhost@GO-MARIADB.TEST', 'tls_certificate_sha256':'b'*64,
            'fixture_inputs':{name:source['workload']['validation/auth_fixture/'+name]
                              for name in ('Dockerfile','serve.py','pam_fixture.c')}}


def external_auth_event():
    return {'Package':ci.PACKAGE+'/validation', 'Test':'TestMariaDBExternalAuthentication',
            'Action':'output', 'Output':'EXTERNAL_AUTH '+json.dumps(external_auth_receipt())+'\n'}


def cdc_receipt():
    return {'contract':'cdc-v1', 'server_version':'12.3.3-MariaDB', 'server_id':621129,
            'transport':'private-unix-socket', 'binlog_format':'ROW', 'checksum':'CRC32',
            'semisync_available':True,
            'fixture_sha256':record.inputs()['workload']['validation/cdc_fixture.py']}


def cdc_event():
    return {'Package':ci.PACKAGE+'/validation', 'Test':'TestMariaDBCDC', 'Action':'output',
            'Output':'CDC_FIXTURE '+json.dumps(cdc_receipt())+'\n'}


def events(extra=()):
    records = [external_auth_event(), cdc_event()]
    for name in ci.REQUIRED:
        package, _, test = name.rpartition('/Test')
        records.append({'Package':package, 'Test':'Test'+test, 'Action':'pass'})
    return '\n'.join(json.dumps(record) for record in [*records, *extra])


class CIPolicyTests(unittest.TestCase):
    def test_missing_fixture_suite_is_rejected(self):
        issues, _ = ci.assess_tests('', 0)
        self.assertTrue(issues)

    def test_prior_gap_is_no_longer_an_allowed_failure(self):
        case = {'Package':ci.PACKAGE+'/validation', 'Test':'TestMariaDBDesiredCleanPoolReset'}
        output = events([{**case, 'Action':'output', 'Output':'GAP: pool checkout retains previous request user variables; ResetSession is liveness-only'},
                         {**case, 'Action':'fail'}, {'Package':ci.PACKAGE+'/validation', 'Action':'fail'}])
        issues, records = ci.assess_tests(output, 1)
        self.assertTrue(issues)
        self.assertEqual(records[ci.MANAGED_SESSION_TEST]['status'], 'fail')

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

    def test_passing_fault_parent_cannot_hide_a_skipped_required_subtest(self):
        output=events([{'Package':ci.PACKAGE,'Test':'TestNativeCursorMaterializationError/legacy=true','Action':'skip'}])
        self.assertTrue(any('Required fault test skipped' in issue for issue in ci.assess_tests(output,0)[0]))
