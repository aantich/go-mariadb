#!/usr/bin/env python3
"""Provision only reserved role fixtures; instrument only an isolated server."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess


def provision(config, admin, isolated=False):
    if config['database'] != 'go_mariadb_validation' or config['upstream_database'] != 'go_mariadb_upstream':
        raise ValueError('Refusing non-reserved session fixture schemas.')
    password = config['password']
    if not password or any(c not in '0123456789abcdef' for c in password):
        raise ValueError('Expected private hexadecimal fixture credentials.')
    if isolated and os.environ.get('GO_MARIADB_ISOLATED_CI') != '1':
        raise ValueError('Instrumentation provisioning requires the isolated-CI marker.')
    admin("CREATE ROLE IF NOT EXISTS gomdb_session_role")
    admin("GRANT SELECT ON go_mariadb_validation.* TO gomdb_session_role")
    admin(f"CREATE USER IF NOT EXISTS 'gomdb_roleonly'@'localhost' IDENTIFIED BY '{password}'")
    admin(f"ALTER USER 'gomdb_roleonly'@'localhost' IDENTIFIED BY '{password}'")
    admin("GRANT gomdb_session_role TO 'gomdb_roleonly'@'localhost'")
    admin("SET DEFAULT ROLE NONE FOR 'gomdb_roleonly'@'localhost'")
    if isolated:
        if admin('SELECT @@performance_schema').strip() != '1':
            raise ValueError('Isolated server must start with performance_schema enabled.')
        admin("GRANT SELECT ON performance_schema.* TO 'gomdb_validation'@'localhost'")
        admin("UPDATE performance_schema.setup_consumers SET ENABLED='YES' WHERE NAME IN ('events_statements_current','events_statements_history')")
        admin("UPDATE performance_schema.setup_instruments SET ENABLED='YES',TIMED='YES' WHERE NAME LIKE 'statement/%'")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, default=Path(__file__).parent/'.local/config.json')
    parser.add_argument('--isolated', action='store_true')
    args = parser.parse_args()
    config = json.loads(args.config.read_text())
    client = shutil.which('mariadb') or shutil.which('mysql')
    if not client:
        raise SystemExit('MariaDB administrative client is required.')
    command = [client,'--batch','--skip-column-names','--protocol=socket']
    if args.isolated:
        command = [client,'--no-defaults','--protocol=tcp','--host=127.0.0.1','--user=root','--skip-ssl','--batch','--skip-column-names']
    def admin(sql):
        result = subprocess.run(command, input=sql, text=True, capture_output=True, timeout=30)
        if result.returncode:
            raise SystemExit('Reserved session fixture SQL failed; credentials and SQL are not echoed.')
        return result.stdout.strip()
    provision(config, admin, args.isolated)
    print('Reserved role/session fixtures ready; shared-server global settings unchanged.')


if __name__ == '__main__':
    main()
