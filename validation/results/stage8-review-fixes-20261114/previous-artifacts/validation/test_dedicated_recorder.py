"""Guard comparison identity and discovery of untracked implementation files."""
import json
from pathlib import Path
from unittest import TestCase, mock
import record
import tempfile


class DedicatedRecorderTests(TestCase):
    def test_original_workloads_are_unchanged_after_identity_migration(self):
        baseline = json.loads((record.HERE/'baselines'/record.PROFILE/'capture.json').read_text())
        self.assertTrue(record.baseline_workloads_match(baseline))

    def test_body_change_is_rejected_even_when_identity_migration_is_allowed(self):
        baseline = {'inputs': {'workload': {'probe.go': record.digest(b'package mysql\nfunc f() { original() }\n')}}}
        with mock.patch.object(record, 'original_workload_bytes', return_value=b'package mysql\nfunc f() { changed() }\n'):
            self.assertFalse(record.baseline_workloads_match(baseline))

    def test_new_production_files_are_fingerprinted_before_commit(self):
        self.assertIn('bulk.go', record.inputs()['driver'])
        self.assertIn('auth_parsec.go', record.inputs()['driver'])

    def test_case_identity_changes_only_the_module_namespace(self):
        self.assertEqual(record.case_key('github.com/aantich/go-mariadb/validation/TestX/sub'),
                         'github.com/go-sql-driver/mysql/validation/TestX/sub')

    def test_new_native_workload_changes_cannot_reuse_old_timings(self):
        reference = {'native_workload_inputs': {'native_test.go': 'original-hash'}}
        self.assertTrue(record.native_workloads_match(reference, {'workload': {'native_test.go': 'original-hash'}}))
        self.assertFalse(record.native_workloads_match(reference, {'workload': {'native_test.go': 'changed-hash'}}))
        self.assertFalse(record.native_workloads_match({}, {'workload': {}}))

    def test_tampered_feature_reference_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'capture.json').write_bytes(b'original measurement')
            (root/'SHA256SUMS').write_text(record.digest(b'original measurement')+'  capture.json\n')
            self.assertTrue(record.reference_hashes_match(root))
            (root/'capture.json').write_bytes(b'changed measurement')
            self.assertFalse(record.reference_hashes_match(root))

    def test_interrupted_run_keeps_inputs_without_publishing_a_complete_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            record.checkpoint(root, {'inputs': {'driver': {'bulk.go': 'source-hash'}}, 'complete': True}, {'command': 'race'})
            partial=json.loads((root/'capture.partial.json').read_text())
            self.assertFalse(partial['complete'])
            self.assertEqual(partial['inputs']['driver']['bulk.go'], 'source-hash')
            self.assertFalse((root/'capture.json').exists())
