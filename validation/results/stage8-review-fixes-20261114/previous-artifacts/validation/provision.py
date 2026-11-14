#!/usr/bin/env python3
"""Create reserved Go-driver fixtures on the existing local MariaDB service.

Only task-owned schemas/accounts are modified. No global server setting or
application account is changed. Credentials stay in an owner-only ignored file.
"""
import getpass
import json
from pathlib import Path
import secrets
import subprocess

HERE = Path(__file__).resolve().parent
PRIVATE = HERE / '.local' / 'config.json'


def admin(sql):
    p = subprocess.run(['mariadb', '--protocol=socket', '--batch', '--skip-column-names'],
                       input=sql, text=True, capture_output=True)
    if p.returncode:
        # SQL can contain generated credentials: never echo the command/input.
        raise SystemExit('MariaDB fixture provisioning failed: ' + p.stderr)
    return p.stdout.strip()


def main():
    version = admin('SELECT VERSION()')
    if 'MariaDB' not in version:
        raise SystemExit('A running MariaDB server is required before provisioning.')
    cfg = json.loads(PRIVATE.read_text()) if PRIVATE.exists() else {'password': secrets.token_hex(24)}
    if any(c not in '0123456789abcdef' for c in cfg['password']):
        raise SystemExit('Unexpected local credential format.')
    cfg.update(host='127.0.0.1', port=3306, socket='/tmp/mysql.sock',
               user='gomdb_validation', database='go_mariadb_validation',
               upstream_database='go_mariadb_upstream',
               tls_ca='/opt/homebrew/var/mariadb-haskell/tls/ca.pem',
               tls_cert='/opt/homebrew/var/mariadb-haskell/tls/client-cert.pem',
               tls_key='/opt/homebrew/var/mariadb-haskell/tls/client-key.pem')
    pw = cfg['password']
    for schema in [cfg['database'], cfg['upstream_database']]:
        admin(f'CREATE DATABASE IF NOT EXISTS `{schema}` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci')
    plugins = {line.split('\t')[0] for line in admin(
        "SELECT PLUGIN_NAME FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_TYPE='AUTHENTICATION' AND PLUGIN_STATUS='ACTIVE'").splitlines()}
    cfg['plugins'] = sorted(plugins)
    accounts = {'gomdb_validation': f"IDENTIFIED BY '{pw}'",
                'gomdb_x509': f"IDENTIFIED BY '{pw}' REQUIRE X509",
                'gomdb_socket': "IDENTIFIED VIA unix_socket USING '" + getpass.getuser().replace("'", "''") + "'"}
    for plugin in ['ed25519', 'parsec', 'caching_sha2_password']:
        if plugin in plugins:
            accounts['gomdb_' + plugin] = f"IDENTIFIED VIA {plugin} USING PASSWORD('{pw}')"
    for account, clause in accounts.items():
        admin(f"CREATE USER IF NOT EXISTS '{account}'@'localhost' {clause}; ALTER USER '{account}'@'localhost' {clause}")
        for schema in [cfg['database'], cfg['upstream_database']]:
            admin(f"GRANT ALL ON `{schema}`.* TO '{account}'@'localhost'")
    PRIVATE.parent.mkdir(parents=True, exist_ok=True)
    PRIVATE.parent.chmod(0o700)
    fd = PRIVATE.open('w')
    PRIVATE.chmod(0o600)
    with fd:
        json.dump(cfg, fd, indent=2)
        fd.write('\n')
    print(f'MariaDB {version}: isolated Go schemas/accounts ready; credentials saved privately.')


if __name__ == '__main__':
    main()
