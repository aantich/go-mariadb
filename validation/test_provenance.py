"""Server snapshots and optional Git metadata must survive source-only builds."""
import json
from pathlib import Path
import subprocess
import unittest
from unittest import mock

import provenance


def sample_server():
    server = {name:'test' for name in provenance.SERVER_STRINGS}
    server.update({name:1 for name in provenance.SERVER_NUMBERS})
    server.update(version='12.3.3-MariaDB', database='go_mariadb_validation',
                  sql_mode='STRICT_TRANS_TABLES', time_zone='SYSTEM',
                  character_set_client='utf8mb4', collation_connection='utf8mb4_general_ci',
                  character_set_server='utf8mb4', collation_server='utf8mb4_unicode_ci')
    return server


class ProvenanceTests(unittest.TestCase):
    def test_git_unavailable_is_not_a_check_failure(self):
        with mock.patch.object(provenance.shutil,'which',return_value=None):
            result = provenance.revision_info(Path('.'))
        self.assertIsNone(result['commit'])
        self.assertEqual(result['revision_provenance']['status'],'git-unavailable')

    def test_installed_git_without_metadata_is_not_a_check_failure(self):
        with mock.patch.object(provenance.shutil,'which',return_value='/usr/bin/git'), \
                mock.patch.object(provenance.subprocess,'run',return_value=subprocess.CompletedProcess([],128,'','not a git repository')):
            result = provenance.revision_info(Path('.'))
        self.assertIsNone(result['commit'])
        self.assertEqual(result['revision_provenance']['status'],'metadata-unavailable')
        self.assertNotEqual(result['revision_provenance']['exit_code'],0)

    def test_git_revision_and_lookup_timeout(self):
        commit = 'a'*40
        with mock.patch.object(provenance.shutil,'which',return_value='/usr/bin/git'):
            with mock.patch.object(provenance.subprocess,'run',return_value=subprocess.CompletedProcess([],0,commit+'\n','')):
                self.assertEqual(provenance.revision_info(Path('.'))['commit'],commit)
            with mock.patch.object(provenance.subprocess,'run',side_effect=subprocess.TimeoutExpired('git',10)):
                self.assertEqual(provenance.revision_info(Path('.'))['revision_provenance']['status'],'lookup-unavailable')

    def test_server_parser_requires_complete_mariadb_snapshot(self):
        server = sample_server()
        self.assertEqual(provenance.parse_server(json.dumps(server)),server)
        for changed in ({**server,'version':'8.4.0'}, {**server,'database':'application'},
                        {**server,'performance_schema':'ON'}, {**server,'max_connections':True}):
            with self.assertRaises(ValueError):
                provenance.parse_server(json.dumps(changed))
        del server['sql_mode']
        with self.assertRaises(ValueError):
            provenance.parse_server(json.dumps(server))
        for invalid in ('null', '[]', '{"performance_schema": OFF}'):
            with self.assertRaises(ValueError):
                provenance.parse_server(invalid)

    def test_server_query_uses_configured_endpoint_and_private_environment(self):
        config = {'host':'127.0.0.1','port':3307,'user':'gomdb_validation',
                  'database':'go_mariadb_validation','password':'private-fixture-password'}
        command = provenance.server_command(config,'mariadb')
        output = json.dumps(sample_server())+'\n'
        with mock.patch.object(provenance.subprocess,'run',return_value=subprocess.CompletedProcess(command,0,output,'')) as run:
            provenance.read_server(config,{'GOMAXPROCS':'2'},command)
        self.assertIn('--port=3307',command)
        self.assertIn('--host=127.0.0.1',command)
        self.assertIn('--no-defaults',command)
        self.assertIn("'performance_schema',(@@performance_schema+0)",provenance.SERVER_QUERY)
        self.assertNotIn(config['password'],' '.join(command))
        self.assertEqual(run.call_args.kwargs['env']['MYSQL_PWD'],config['password'])
        with mock.patch.object(provenance.subprocess,'run',return_value=subprocess.CompletedProcess(command,1,'',config['password'])):
            with self.assertRaises(ValueError) as error:
                provenance.read_server(config,{},command)
        self.assertNotIn(config['password'],str(error.exception))
