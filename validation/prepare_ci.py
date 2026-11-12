#!/usr/bin/env python3
"""Prepare disposable Linux CI fixtures; never configure the shared Mac server.

The certificates subcommand runs before the database setup action. Provisioning
requires an explicit isolated-CI marker because it installs authentication
plugins on that disposable server. Ordinary local validation uses provision.py.
"""
import argparse
import getpass
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
from prepare_sessions import provision as provision_sessions


def certificates(directory):
    directory.mkdir(parents=True, exist_ok=True)
    directory.chmod(0o700)
    os.umask(0o077)
    def openssl(*args):
        subprocess.run(['openssl', *args], cwd=directory, check=True, capture_output=True)
    openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2', '-subj', '/CN=Go MariaDB CI CA',
            '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign',
            '-keyout', 'ca-key.pem', '-out', 'ca.pem')
    for role in ('server','client'):
        openssl('req', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=localhost', '-keyout', role+'-key.pem', '-out', role+'.csr')
        extension = 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage='+role+'Auth\n'
        if role == 'server':
            extension += 'subjectAltName=DNS:localhost,IP:127.0.0.1\n'
        (directory/(role+'.ext')).write_text(extension)
        openssl('x509', '-req', '-in', role+'.csr', '-CA', 'ca.pem', '-CAkey', 'ca-key.pem', '-set_serial',
                '2' if role == 'server' else '3', '-days', '2', '-extfile', role+'.ext', '-out', role+'-cert.pem')


def provision(directory, output):
    if os.environ.get('GO_MARIADB_ISOLATED_CI') != '1':
        raise SystemExit('Plugin provisioning is restricted to an explicitly isolated CI server.')
    client = shutil.which('mariadb') or shutil.which('mysql')
    if not client:
        raise SystemExit('MariaDB command-line client is required.')
    def admin(sql):
        result = subprocess.run([client, '--no-defaults', '--protocol=tcp', '--host=127.0.0.1', '--user=root',
                                 '--skip-ssl', '--batch', '--skip-column-names'], input=sql, text=True, capture_output=True, timeout=30)
        if result.returncode:
            # SQL can include generated account credentials. Do not echo it.
            raise SystemExit('Isolated fixture SQL failed; inspect the disposable server log.')
        return result.stdout.strip()
    version = admin('SELECT VERSION()')
    if 'MariaDB' not in version:
        raise SystemExit('A disposable MariaDB server is required.')
    plugins = {'ed25519':'auth_ed25519', 'parsec':'auth_parsec',
               'caching_sha2_password':'auth_mysql_sha2', 'unix_socket':'auth_socket'}
    active = set(admin("SELECT PLUGIN_NAME FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_TYPE='AUTHENTICATION' AND PLUGIN_STATUS='ACTIVE'").splitlines())
    for plugin, library in plugins.items():
        if plugin not in active:
            admin("INSTALL SONAME '"+library+"'")
    password = secrets.token_hex(24)
    schemas = ('go_mariadb_validation', 'go_mariadb_upstream')
    for schema in schemas:
        admin(f'CREATE DATABASE IF NOT EXISTS `{schema}` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci')
    accounts = {'gomdb_validation':f"IDENTIFIED BY '{password}'",
                'gomdb_x509':f"IDENTIFIED BY '{password}' REQUIRE X509",
                'gomdb_socket':"IDENTIFIED VIA unix_socket USING '"+getpass.getuser().replace("'", "''")+"'"}
    for plugin in ('ed25519','parsec','caching_sha2_password'):
        accounts['gomdb_'+plugin] = f"IDENTIFIED VIA {plugin} USING PASSWORD('{password}')"
    for account, clause in accounts.items():
        admin(f"CREATE USER IF NOT EXISTS '{account}'@'localhost' {clause}; ALTER USER '{account}'@'localhost' {clause}")
        for schema in schemas:
            admin(f"GRANT ALL ON `{schema}`.* TO '{account}'@'localhost'")
    config = {'host':'127.0.0.1', 'port':3306, 'socket':admin('SELECT @@socket'),
              'user':'gomdb_validation', 'password':password, 'database':schemas[0], 'upstream_database':schemas[1],
              'tls_ca':str(directory/'ca.pem'), 'tls_cert':str(directory/'client-cert.pem'), 'tls_key':str(directory/'client-key.pem'),
              'plugins':sorted(plugins)}
    provision_sessions(config, admin, isolated=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.parent.chmod(0o700)
    # Set the mode at creation, before writing any credentials.
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.chmod(output, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(config, stream, indent=2)
        stream.write('\n')
    print('Isolated MariaDB '+version+' fixtures ready; native/TLS/auth tests are required.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=('certificates','provision'))
    parser.add_argument('--tls-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, default=Path(__file__).parent/'.local/ci-config.json')
    args = parser.parse_args()
    if args.operation == 'certificates':
        certificates(args.tls_dir.resolve())
    else:
        provision(args.tls_dir.resolve(), args.output.resolve())


if __name__ == '__main__':
    main()
