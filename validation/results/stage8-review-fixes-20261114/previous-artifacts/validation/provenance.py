"""Read-only server and optional revision provenance for correctness receipts."""
import json
from pathlib import Path
import re
import shutil
import subprocess

# JSON preserves SQL modes/comments containing tabs or newlines. Use the same
# endpoint/account/schema as the Go suite, never an unrelated default socket.
SERVER_QUERY = "SELECT JSON_OBJECT(" + ','.join(
    "'"+name+"',"+expression for name, expression in (
        ('version', 'VERSION()'), ('version_comment', '@@version_comment'),
        # Boolean system variables can serialize as unquoted ON/OFF in MariaDB
        # JSON_OBJECT. Arithmetic forces JSON numbers on all supported servers.
        ('database', 'DATABASE()'), ('performance_schema', '(@@performance_schema+0)'),
        ('max_allowed_packet', '@@max_allowed_packet'), ('local_infile', '(@@local_infile+0)'),
        ('log_bin', '(@@log_bin+0)'), ('sql_mode', '@@sql_mode'), ('time_zone', '@@time_zone'),
        ('character_set_client', '@@character_set_client'),
        ('collation_connection', '@@collation_connection'),
        ('character_set_server', '@@character_set_server'), ('collation_server', '@@collation_server'),
        ('innodb_flush_log_at_trx_commit', '@@innodb_flush_log_at_trx_commit'),
        ('sync_binlog', '@@sync_binlog'), ('max_connections', '@@max_connections'),
    )) + ')'
SERVER_STRINGS = {'version', 'version_comment', 'database', 'sql_mode', 'time_zone',
                  'character_set_client', 'collation_connection', 'character_set_server', 'collation_server'}
SERVER_NUMBERS = {'performance_schema', 'max_allowed_packet', 'local_infile', 'log_bin',
                  'innodb_flush_log_at_trx_commit', 'sync_binlog', 'max_connections'}


def revision_info(root):
    """Git is optional for source exports; byte fingerprints remain mandatory."""
    if not shutil.which('git'):
        return {'commit': None, 'revision_provenance': {'status': 'git-unavailable'}}
    try:
        result = subprocess.run(['git', 'rev-parse', '--verify', 'HEAD'], cwd=root,
                                text=True, capture_output=True, timeout=10)
    except (OSError, subprocess.TimeoutExpired):
        return {'commit': None, 'revision_provenance': {'status': 'lookup-unavailable'}}
    commit = result.stdout.strip()
    if result.returncode or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', commit):
        return {'commit': None, 'revision_provenance': {
            'status': 'metadata-unavailable', 'exit_code': result.returncode}}
    return {'commit': commit, 'revision_provenance': {'status': 'available'}}


def server_command(endpoint, program):
    return [program, '--no-defaults', '--protocol=tcp', '--host='+endpoint['host'],
            '--port='+str(endpoint['port']), '--user='+endpoint['user'],
            '--database='+endpoint['database'], '--batch', '--raw', '--skip-column-names',
            '--skip-ssl', '--execute='+SERVER_QUERY]


def parse_server(output):
    snapshot = json.loads(output)
    if not isinstance(snapshot, dict) or set(snapshot) != SERVER_STRINGS | SERVER_NUMBERS:
        raise ValueError('Incomplete server provenance.')
    if any(not isinstance(snapshot[key], str) for key in SERVER_STRINGS):
        raise ValueError('Invalid server string setting.')
    if any(type(snapshot[key]) is not int or snapshot[key] < 0 for key in SERVER_NUMBERS):
        raise ValueError('Invalid server numeric setting.')
    if 'MariaDB' not in snapshot['version'] or snapshot['database'] != 'go_mariadb_validation':
        raise ValueError('Expected MariaDB in the reserved validation schema.')
    return snapshot


def read_server(config, env, command):
    # Credentials never enter argv, capture metadata or error messages. The
    # subprocess environment is private; no shell interpolation is involved.
    private_env = {**env, 'MYSQL_PWD': config['password']}
    result = subprocess.run(command, env=private_env, text=True, capture_output=True, timeout=30)
    if result.returncode:
        raise ValueError('MariaDB provenance query failed; credentials are not echoed.')
    output = result.stdout.replace(config['password'], '[redacted]')
    return parse_server(output), output


def validate_server(capture, directory):
    """Require both raw snapshots to corroborate the recorded environment."""
    server = capture.get('server', {})
    endpoint = server.get('endpoint', {})
    if (set(endpoint) != {'host', 'port', 'user', 'database'} or
            not all(isinstance(endpoint.get(key), str) and endpoint[key] for key in ('host', 'user', 'database')) or
            type(endpoint.get('port')) is not int or not 0 < endpoint['port'] < 65536 or
            endpoint['database'] != 'go_mariadb_validation'):
        raise ValueError('Missing or invalid server endpoint provenance.')
    command = server.get('command', [])
    if (not isinstance(command, list) or not command or not all(isinstance(arg, str) for arg in command) or
            Path(command[0]).name not in ('mariadb', 'mysql') or
            command != server_command(endpoint, command[0])):
        raise ValueError('Incorrect server provenance command.')
    for phase in ('before', 'after'):
        observed = parse_server((directory/('server-'+phase+'.log')).read_text())
        if observed != server.get(phase):
            raise ValueError('Raw server provenance does not match its receipt.')
    if server['before'] != server['after'] or server.get('stable') is not True:
        raise ValueError('Server settings changed during correctness checks.')
    if capture.get('instrumentation_required') and server['before']['performance_schema'] != 1:
        raise ValueError('Required server instrumentation is disabled.')
    return server['before']
