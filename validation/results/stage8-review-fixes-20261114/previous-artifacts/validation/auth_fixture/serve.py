#!/usr/bin/env python3
"""Owned PAM/Kerberos/MariaDB services in a disposable Linux container.

Nothing in this module is intended to run on a developer's host. Generated
keys, ticket caches, test PAM policies and users exist only in this fixture.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path(os.environ.get('GO_MARIADB_AUTH_FIXTURE_ROOT', '/fixture'))
PORT = int(os.environ.get('GO_MARIADB_AUTH_FIXTURE_PORT', '3306'))
REALM = 'GO-MARIADB.TEST'
SERVICE = f'mariadb/localhost@{REALM}'


def command(args, **kwargs):
    return subprocess.run(list(map(str, args)), check=True, **kwargs)


def main():
    ROOT.mkdir(mode=0o700, exist_ok=True)
    os.environ.update(KRB5_CONFIG=str(ROOT/'krb5.conf'),
                      KRB5_KDC_PROFILE=str(ROOT/'kdc.conf'))
    (ROOT/'krb5.conf').write_text(f'''[libdefaults]
 default_realm = {REALM}
 dns_lookup_kdc = false
 dns_lookup_realm = false
 rdns = false
 udp_preference_limit = 1
[realms]
 {REALM} = {{
  kdc = 127.0.0.1:10088
 }}
''')
    (ROOT/'kdc.conf').write_text(f'''[kdcdefaults]
 kdc_ports = 10088
 kdc_tcp_ports = 10088
[realms]
 {REALM} = {{
  database_name = {ROOT}/principal
  key_stash_file = {ROOT}/stash
  acl_file = {ROOT}/kadm5.acl
  max_life = 1d
 }}
''')
    (ROOT/'kadm5.acl').write_text('')
    command(['kdb5_util', 'create', '-s', '-P', 'stage7-disposable-master'])
    for principal in [SERVICE, f'driver@{REALM}', f'other@{REALM}']:
        command(['kadmin.local', '-q', f'addprinc -randkey {principal}'])
    command(['kadmin.local', '-q', f'ktadd -k {ROOT}/server.keytab {SERVICE}'])
    for name in ['driver', 'other']:
        command(['kadmin.local', '-q', f'ktadd -k {ROOT}/{name}.keytab {name}@{REALM}'])
    for service, argument in [('single', ''), ('multi', ' two-factor')]:
        Path(f'/etc/pam.d/go-mariadb-{service}').write_text(
            'auth required /opt/driver-auth/pam_fixture.so'+argument+'\n'
            'account required pam_permit.so\n')
    command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
             '-keyout', ROOT/'tls-key.pem', '-out', ROOT/'tls-cert.pem',
             '-days', '7', '-subj', '/CN=localhost',
             '-addext', 'subjectAltName=DNS:localhost,IP:127.0.0.1'],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    command(['mariadb-install-db', '--no-defaults', f'--datadir={ROOT}/data',
             '--auth-root-authentication-method=normal', '--skip-test-db'],
            stdout=subprocess.DEVNULL)
    processes = []
    logs = []
    def stop(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        for name, args in [('kdc', ['krb5kdc', '-n']), ('server', [
            'mariadbd', '--no-defaults', '--user=root', f'--datadir={ROOT}/data',
            f'--socket={ROOT}/server.sock', f'--pid-file={ROOT}/server.pid',
            '--bind-address=0.0.0.0', f'--port={PORT}',
            f'--ssl-cert={ROOT}/tls-cert.pem', f'--ssl-key={ROOT}/tls-key.pem',
            '--plugin-load-add=auth_pam', '--plugin-load-add=auth_gssapi',
            f'--gssapi-principal-name={SERVICE}', f'--gssapi-keytab-path={ROOT}/server.keytab',
            '--character-set-server=utf8mb4', '--collation-server=utf8mb4_unicode_ci'])]:
            log = (ROOT/f'{name}.log').open('w')
            logs.append(log)
            processes.append(subprocess.Popen(args, stdout=log, stderr=subprocess.STDOUT))
        client = ['mariadb', '--no-defaults', f'--socket={ROOT}/server.sock', '-uroot']
        for _ in range(120):
            if any(p.poll() is not None for p in processes):
                raise RuntimeError('Authentication fixture service exited; inspect /fixture/*.log')
            ready = subprocess.run(client+['-e', 'SELECT 1'], capture_output=True)
            if ready.returncode == 0:
                break
            time.sleep(0.25)
        else:
            raise RuntimeError('Authentication fixture startup deadline exceeded')
        command(client, input=f'''
CREATE DATABASE go_mariadb_validation;
CREATE USER 'gomdb_pam'@'%' IDENTIFIED VIA pam USING 'go-mariadb-single';
CREATE USER 'gomdb_pam_multi'@'%' IDENTIFIED VIA pam USING 'go-mariadb-multi';
CREATE USER 'gomdb_gssapi'@'%' IDENTIFIED VIA gssapi AS 'driver@{REALM}';
GRANT ALL ON go_mariadb_validation.* TO 'gomdb_pam'@'%','gomdb_pam_multi'@'%','gomdb_gssapi'@'%';
''', text=True)
        # Populate service tickets too: host clients can authenticate without
        # changing host DNS/realm configuration or contacting a different KDC.
        for name in ['driver', 'other']:
            cache = f'FILE:{ROOT}/{name}.ccache'
            command(['kinit', '-c', cache, '-kt', ROOT/f'{name}.keytab', f'{name}@{REALM}'])
            command(['kvno', '-c', cache, SERVICE])
        version = subprocess.check_output(client+['--batch', '--skip-column-names', '-e', 'SELECT VERSION()'], text=True).strip()
        (ROOT/'ready.json').write_text(json.dumps({'realm': REALM, 'principal': SERVICE, 'version': version,
            'password': 'stage7-test-password', 'code': '246810'}))
        while True:
            if any(p.poll() is not None for p in processes):
                raise RuntimeError('Authentication fixture service stopped')
            time.sleep(1)
    except KeyboardInterrupt:
        pass
    except Exception:
        import sys
        for filename in ['kdc.log', 'server.log']:
            path = ROOT/filename
            if path.is_file():
                print(filename+":\n"+path.read_text()[-12000:], file=sys.stderr)
        raise
    finally:
        for p in reversed(processes):
            p.terminate()
        for p in reversed(processes):
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
        for log in logs:
            log.close()


if __name__ == '__main__':
    main()
