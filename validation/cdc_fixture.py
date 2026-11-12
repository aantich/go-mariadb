#!/usr/bin/env python3
"""Run correctness against an owned, socket-only binlog/semisync MariaDB.

No shared server configuration or application data is touched. The temporary
datadir, TLS keys, control files and socket live in a private directory. Child
and server have bounded shutdown; restart requests can target only this server.
"""
import hashlib
import json
import os
import signal
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

from prepare_ci import certificates


def stop(process, *, group=False, grace=2.0):
    if process is None:
        return
    if group:
        # start_new_session=True makes this child's PID its owned process-group
        # ID. The leader may already be reaped while descendants still exist.
        def send(sig):
            try:
                os.killpg(process.pid, sig)
                return True
            except ProcessLookupError:
                return False
        if send(signal.SIGTERM):
            deadline = time.monotonic()+grace
            while send(0) and time.monotonic() < deadline:
                process.poll()  # Reap our direct child while waiting on its group.
                time.sleep(0.025)
            # SIGKILL also covers descendants that ignored SIGTERM. Orphaned
            # zombies can retain a group ID until init reaps them, so do not
            # wait indefinitely for killpg(0) to report the group gone.
            send(signal.SIGKILL)
        process.wait(timeout=10)
    elif process.poll() is None:
        def send(sig):
            try:
                if sig == signal.SIGTERM:
                    process.terminate()
                else:
                    process.kill()
            except ProcessLookupError:
                pass
        send(signal.SIGTERM)
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            send(signal.SIGKILL)
            process.wait(timeout=10)


def toolchain():
    """Select one MariaDB installation, never an unrelated PATH initializer.

    Standalone/Homebrew installs keep initialization scripts under scripts/;
    system packages use bin/. Resolve a server symlink before inferring its
    prefix. CI can pin that prefix explicitly with GO_MARIADB_CDC_BASEDIR.
    """
    selected = os.environ.get('GO_MARIADB_CDC_BASEDIR')
    if selected:
        basedir = Path(selected).resolve()
    else:
        server = shutil.which('mariadbd')
        if not server:
            raise RuntimeError('MariaDB server binaries are required for isolated CDC checks')
        basedir = Path(server).resolve().parent.parent

    def find(candidates):
        for relative in candidates:
            path = basedir/relative
            if path.is_file() and os.access(path, os.X_OK):
                return str(path)
        raise RuntimeError('Incomplete MariaDB CDC toolchain under '+str(basedir))

    return basedir, {
        'mariadbd':find(('bin/mariadbd', 'sbin/mariadbd', 'libexec/mariadbd')),
        'mariadb':find(('bin/mariadb',)),
        'mariadb-install-db':find(('scripts/mariadb-install-db', 'bin/mariadb-install-db',
                                  'scripts/mysql_install_db', 'bin/mysql_install_db'))}


def main(arguments):
    if arguments and arguments[0] == '--':
        arguments = arguments[1:]
    if not arguments:
        raise ValueError('usage: cdc_fixture.py -- COMMAND [ARGUMENT ...]')
    basedir, programs = toolchain()
    with tempfile.TemporaryDirectory(prefix='go-mariadb-cdc-') as temporary:
        directory = Path(temporary)
        directory.chmod(0o700)
        socket = directory/'server.sock'
        tls = directory/'tls'
        certificates(tls)
        with (directory/'server.log').open('w+') as log:
            try:
                subprocess.run([programs['mariadb-install-db'], '--no-defaults', '--basedir='+str(basedir), '--datadir='+str(directory/'data'),
                                '--auth-root-authentication-method=normal', '--skip-test-db'],
                               stdout=log, stderr=subprocess.STDOUT, check=True, timeout=60)
            except subprocess.SubprocessError:
                # Initialization precedes the running-server cleanup block.
                # Keep its diagnostics visible before the temporary log vanishes.
                log.flush()
                log.seek(0)
                print(log.read()[-4000:], file=sys.stderr)
                raise
            command = [programs['mariadbd'], '--no-defaults', '--basedir='+str(basedir), '--datadir='+str(directory/'data'),
                       '--socket='+str(socket), '--pid-file='+str(directory/'server.pid'), '--skip-networking',
                       '--server-id=621129', '--log-bin='+str(directory/'bin'), '--binlog-format=ROW',
                       '--binlog-row-image=FULL', '--binlog-row-metadata=FULL', '--binlog-checksum=CRC32',
                       '--plugin-load-add=semisync_master', '--rpl-semi-sync-master-timeout=2000',
                       '--rpl-semi-sync-master-wait-no-slave=OFF', '--innodb-buffer-pool-size=64M',
                       '--max-allowed-packet=48M',
                       '--ssl-ca='+str(tls/'ca.pem'), '--ssl-cert='+str(tls/'server-cert.pem'),
                       '--ssl-key='+str(tls/'server-key.pem'), '--default-time-zone=+00:00']
            if os.geteuid() == 0:
                command.append('--user=root')
            server = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
            child = None

            def sql(statement):
                result = subprocess.run([programs['mariadb'], '--no-defaults', '--protocol=socket',
                    '--socket='+str(socket), '--user=root', '--batch', '--skip-column-names'],
                    input=statement, text=True, capture_output=True, timeout=8)
                return result

            def ready():
                deadline = time.monotonic()+35
                while time.monotonic() < deadline:
                    if sql('SELECT 1').returncode == 0:
                        return
                    if server.poll() is not None:
                        raise RuntimeError('owned CDC server stopped before readiness')
                    time.sleep(0.1)
                raise RuntimeError('owned CDC server readiness deadline')

            try:
                ready()
                setup = sql('CREATE DATABASE go_mariadb_cdc')
                if setup.returncode:
                    raise RuntimeError('owned CDC schema setup failed')
                version = sql('SELECT VERSION()').stdout.strip()
                if 'MariaDB' not in version:
                    raise RuntimeError('CDC fixture requires MariaDB')
                receipt = {'contract':'cdc-v1', 'server_version':version, 'server_id':621129,
                           'transport':'private-unix-socket', 'binlog_format':'ROW', 'checksum':'CRC32',
                           'semisync_available':True,
                           'fixture_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
                env = {**os.environ, 'GO_MARIADB_CDC_SOCKET':str(socket),
                       'GO_MARIADB_CDC_CONTROL':str(directory), 'GO_MARIADB_CDC_TLS_CA':str(tls/'ca.pem'),
                       'GO_MARIADB_CDC_RECEIPT':json.dumps(receipt, sort_keys=True)}
                # This session contains only our correctness child and its Go
                # workers. Reap the group on timeout, never the caller's group.
                child = subprocess.Popen(arguments, env=env, start_new_session=True)
                deadline = time.monotonic()+1200
                while child.poll() is None:
                    if time.monotonic() >= deadline:
                        raise RuntimeError('CDC correctness child deadline exceeded')
                    request = directory/'restart.request'
                    if request.exists():
                        request.unlink()
                        stop(server)
                        server = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
                        ready()
                        (directory/'restart.done').touch()
                    time.sleep(0.025)
                return child.returncode
            except BaseException:
                log.flush()
                log.seek(0)
                print(log.read()[-4000:], file=sys.stderr)
                raise
            finally:
                try:
                    stop(child, group=True)
                finally:
                    stop(server)


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))
