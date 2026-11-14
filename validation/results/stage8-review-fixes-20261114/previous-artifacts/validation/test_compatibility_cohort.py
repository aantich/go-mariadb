"""The live compatibility claim must be corroborated by twelve distinct cells."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import compatibility as matrix
import compatibility_cohort as cohort
import record
import test_compatibility


class CompatibilityCohortTests(unittest.TestCase):
    def setUp(self):
        source=record.inputs()
        locked_images=(matrix.HERE/'compatibility-images.json').read_bytes()
        temporary=self.enterContext(tempfile.TemporaryDirectory(prefix='go-mariadb-cohort-'))
        self.root=Path(temporary).resolve()
        self.enterContext(mock.patch.object(matrix,'HERE',self.root))
        self.enterContext(mock.patch.object(record,'HERE',self.root))
        self.enterContext(mock.patch.object(record,'inputs',return_value=source))
        (self.root/'compatibility-images.json').write_bytes(locked_images)
        self.helper=test_compatibility.CompatibilityEvidenceTests()
        self.directories=[]
        for compiler in cohort.COMPILERS:
            for family in cohort.SERVERS:
                directory=self.root/'results'/f'go{compiler}-mariadb{family}'
                directory.mkdir(parents=True)
                capture=self.helper.valid(directory)
                capture['family']=family
                capture['image_reference']=matrix.images()[family]
                capture['compiler']={'path':f'/synthetic/{compiler}/bin/go',
                    'sha256':str(cohort.COMPILERS.index(compiler)+1)*64,
                    'version':f'go version go{compiler}.1 linux/arm64'}
                for name,command in matrix.commands(capture['compiler']['path']).items():
                    capture['stages'][name]['command']=command
                version=family+'.1-MariaDB'
                for phase in ('before','after'):
                    capture['server'][phase]['version']=version
                    (directory/f'server-{phase}.log').write_text(json.dumps(capture['server'][phase]))
                # Keep the raw attributed marker consistent with the actual
                # synthetic server profile, including unavailable older features.
                events=[]
                for line in (directory/'integration.log').read_text().splitlines():
                    event=json.loads(line)
                    if 'MARIADB_COMPATIBILITY ' in event.get('Output',''):
                        newer=family in ('11.8','12.3')
                        marker={'version':version,'family':family,
                            'expected_features':{key:newer for key in ('bulk_units','parsec','vector')}}
                        event['Output']='MARIADB_COMPATIBILITY '+json.dumps(marker)
                    events.append(event)
                output='\n'.join(map(json.dumps,events))+'\n'
                (directory/'integration.log').write_text(output)
                _,capture['stages']['integration']['cases']=matrix.assess(output,0,'integration')
                self.helper.rehash(capture,directory)
                (directory/'capture.json').write_text(json.dumps(capture))
                self.directories.append(directory)

    def test_complete_cohort_and_published_table_are_verified(self):
        summary=cohort.publish(self.directories)
        self.assertEqual(cohort.verify_current(),summary)
        self.assertEqual(summary['profile_count'],12)
        self.assertEqual(len(summary['artifacts']),84)
        self.assertIn('All 12 compiler/server profiles pass',(self.root/'COMPATIBILITY.md').read_text())

    def test_missing_and_stale_pointer_fail(self):
        with self.assertRaises(FileNotFoundError):cohort.verify_current()
        summary=cohort.build(self.directories);summary['inputs']={}
        with self.assertRaisesRegex(ValueError,'stale compatibility cohort'):cohort.validate(summary)

    def test_missing_and_duplicate_actual_profiles_fail(self):
        for directories in (self.directories[:-1],self.directories[:-1]+[self.directories[0]]):
            with self.subTest(count=len(directories)),self.assertRaises(ValueError):cohort.build(directories)

    def test_changed_counts_identity_hash_and_metadata_fail(self):
        original=cohort.build(self.directories)
        cell='go1.25-mariadb10.11'
        variants=[]
        for key,value in (('profile_count',11),('performance_measured',True),('input_fingerprint','wrong'),
                          ('compilers',{}),('server_versions',{}),('artifacts',{})):
            changed=copy.deepcopy(original);changed[key]=value;variants.append(changed)
        for key,value in (('leaf_counts',{'pass':99999,'fail':0,'skip':0}),('capture_sha256','wrong'),
                          ('compiler_family','1.27'),('server_family','12.3'),('checks',{}),('restart_count',0)):
            changed=copy.deepcopy(original);changed['profiles'][cell][key]=value;variants.append(changed)
        for changed in variants:
            with self.assertRaises(ValueError):cohort.validate(changed)

    def test_profile_cannot_be_relabelled_or_substituted(self):
        summary=cohort.build(self.directories)
        changed=copy.deepcopy(summary)
        changed['profiles']['go1.25-mariadb10.11']['evidence_directory']=summary['profiles']['go1.25-mariadb11.4']['evidence_directory']
        with self.assertRaisesRegex(ValueError,'Duplicate'):cohort.validate(changed)

    def test_raw_log_changes_and_external_paths_fail(self):
        summary=cohort.build(self.directories)
        for relative in ('../outside',str(self.root.parent/'outside')):
            changed=copy.deepcopy(summary)
            changed['profiles']['go1.25-mariadb10.11']['evidence_directory']=relative
            with self.assertRaisesRegex(ValueError,'outside validation/results'):cohort.validate(changed)
        log=self.directories[0]/'integration.log'
        log.write_text(log.read_text()+'\nmodified\n')
        with self.assertRaisesRegex(ValueError,'checksum'):cohort.validate(summary)

    def test_log_symlinks_cannot_escape_the_results_tree(self):
        summary=cohort.build(self.directories)
        outside=self.root/'outside.log';outside.write_text('private synthetic data')
        log=self.directories[0]/'root.log';log.unlink();log.symlink_to(outside)
        with self.assertRaisesRegex(ValueError,'outside validation/results'):cohort.validate(summary)
