#!/usr/bin/env python3
"""Manage a disposable external-authentication fixture and private client files.

The normal Mac workflow uses a container. Linux CI runs the same services inside
its already disposable container. Neither mode changes host PAM/realm settings.
"""
from contextlib import contextmanager
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import uuid

HERE = Path(__file__).resolve().parent
IMAGE = 'go-mariadb-stage5-auth'



def runtime_identity(image):
    """Hash runtime files/configuration, excluding changing provenance indexes."""
    config_keys = ('Env', 'Entrypoint', 'Cmd', 'WorkingDir', 'User', 'ExposedPorts',
                   'Volumes', 'Labels', 'StopSignal', 'Healthcheck', 'OnBuild',
                   'Shell', 'ArgsEscaped')
    fields = {key: image.get(key) for key in ('Architecture', 'Os', 'Variant', 'RootFS')}
    fields['Config'] = {key: image.get('Config', {}).get(key) for key in config_keys}
    # OCI omits the default working directory; Docker inspect spells it ''.
    fields['Config']['WorkingDir'] = fields['Config']['WorkingDir'] or ''
    return 'docker-runtime-sha256:'+hashlib.sha256(json.dumps(fields,
        sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def command(args, **kwargs):
    return subprocess.run(list(map(str, args)), check=True, **kwargs)


def cleanup_container(owner):
    """Remove only a container bearing this invocation's unique ownership label.

    Docker can create a container before run reports a startup error. Probe the
    registered name even on that failure path; a name collision is not ownership.
    Diagnostic failures must never prevent the removal attempt.
    """
    inspected = subprocess.run(['docker', 'inspect', '--format',
        '{{ index .Config.Labels "go-mariadb.fixture-owner" }}', owner],
        capture_output=True, text=True, check=False, timeout=15)
    if inspected.returncode or inspected.stdout.strip() != owner:
        return
    try:
        subprocess.run(['docker', 'logs', owner], check=False, timeout=15)
    except (OSError, subprocess.SubprocessError):
        pass
    finally:
        subprocess.run(['docker', 'rm', '-f', owner], check=True,
                       stdout=subprocess.DEVNULL, timeout=30)


@contextmanager
def environment(*, local=False):
    """Yield fixture-specific environment and clean up only owned resources.

    The caller must keep this scope alive throughout validation. Credentials,
    caches and CA files are private temporary files, never report artifacts.
    """
    # A source checkout may be mounted read-only in the disposable Linux job.
    # Private service/cache state belongs in its writable temporary filesystem.
    private_root = None if local else HERE/'.local'
    if private_root is not None:
        private_root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='go-mariadb-external-auth-', dir=private_root) as directory:
        private = Path(directory)
        owner = 'go-mariadb-auth-'+uuid.uuid4().hex[:12]
        process = None
        container = None
        fixture_root = Path('/fixture')
        try:
            if local:
                # This branch is for the disposable Linux CI image only.
                if not Path('/opt/driver-auth/disposable-fixture').is_file() or not Path('/opt/driver-auth/pam_fixture.so').is_file():
                    raise RuntimeError('local authentication fixtures require the disposable Linux CI image')
                fixture_root = private/'services'
                process = subprocess.Popen(['python3', str(HERE/'auth_fixture/serve.py')],
                    env={**os.environ, 'GO_MARIADB_AUTH_FIXTURE_ROOT': str(fixture_root),
                         'GO_MARIADB_AUTH_FIXTURE_PORT': '3307'})
                port, kdc_port = 3307, 10088
            else:
                # Build once with the documented Dockerfile before running.
                command(['docker', 'image', 'inspect', IMAGE], stdout=subprocess.DEVNULL)
                # Register before the fallible creation call: the daemon may
                # have created the resource when its client fails or is killed.
                container = owner
                command(['docker', 'run', '-d', '--name', owner,
                    '--label', 'go-mariadb.fixture=external-authentication',
                    '--label', 'go-mariadb.fixture-owner='+owner,
                    '--cpus', '2', '--memory', '1g',
                    '--tmpfs', '/fixture:rw,mode=0700,size=536870912',
                    '-p', '127.0.0.1::3306', '-p', '127.0.0.1::10088', IMAGE],
                    stdout=subprocess.DEVNULL)
                def published_port(number):
                    value = subprocess.check_output(['docker', 'port', owner, f'{number}/tcp'], text=True).strip()
                    if not value.startswith('127.0.0.1:'):
                        raise RuntimeError('authentication fixture must be bound to loopback')
                    return int(value.rsplit(':', 1)[1])
                port, kdc_port = published_port(3306), published_port(10088)
            for _ in range(160):
                if local:
                    ready = (fixture_root/'ready.json').is_file()
                    alive = process.poll() is None
                else:
                    ready = subprocess.run(['docker', 'exec', owner, 'test', '-f', '/fixture/ready.json'],
                                           capture_output=True).returncode == 0
                    alive = subprocess.check_output(['docker', 'inspect', '-f', '{{.State.Running}}', owner], text=True).strip() == 'true'
                if ready:
                    break
                if not alive:
                    raise RuntimeError('external authentication fixture stopped before readiness')
                time.sleep(0.25)
            else:
                raise RuntimeError('external authentication fixture readiness deadline exceeded')
            for filename in ['ready.json', 'tls-cert.pem', 'driver.ccache', 'other.ccache']:
                if local:
                    shutil.copyfile(fixture_root/filename, private/filename)
                else:
                    # Docker's archive endpoint can omit tmpfs mount contents.
                    # Read through the owned container process instead; ticket
                    # bytes go directly into private files, never tool output.
                    (private/filename).write_bytes(subprocess.check_output(
                        ['docker', 'exec', owner, 'cat', '/fixture/'+filename]))
                (private/filename).chmod(0o600)
            metadata = json.loads((private/'ready.json').read_text())
            realm = metadata['realm']
            (private/'krb5.conf').write_text(f'''[libdefaults]
 default_realm = {realm}
 dns_lookup_kdc = false
 dns_lookup_realm = false
 rdns = false
 udp_preference_limit = 1
[realms]
 {realm} = {{
  kdc = 127.0.0.1:{kdc_port}
 }}
''')
            if local:
                identity = 'local-disposable-linux-profile'
            else:
                image_id = subprocess.check_output(['docker', 'inspect', '-f', '{{.Image}}', container], text=True).strip()
                image = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image_id], text=True))[0]
                identity = runtime_identity(image)
                # Bind fixture source in this checkout to the image that actually
                # ran; an old local tag must not silently validate newer fixtures.
                for name in ('serve.py', 'pam_fixture.c'):
                    actual = subprocess.check_output(['docker', 'exec', owner, 'sha256sum', '/opt/driver-auth/'+name], text=True).split()[0]
                    if actual != hashlib.sha256((HERE/'auth_fixture'/name).read_bytes()).hexdigest():
                        raise RuntimeError('Authentication fixture source differs; rebuild its image.')
            receipt = {'contract':'external-auth-v1', 'runtime_identity':identity,
                       'server_version':metadata['version'], 'realm':realm,
                       'service_principal':metadata['principal'],
                       'tls_certificate_sha256':hashlib.sha256((private/'tls-cert.pem').read_bytes()).hexdigest(),
                       'fixture_inputs':{name:hashlib.sha256((HERE/'auth_fixture'/name).read_bytes()).hexdigest()
                                         for name in ('Dockerfile','serve.py','pam_fixture.c')}}
            yield {'GO_MARIADB_AUTH_FIXTURE_IMAGE_ID': identity,
                'GO_MARIADB_AUTH_RECEIPT':json.dumps(receipt,sort_keys=True),
                'GO_MARIADB_AUTH_HOST': '127.0.0.1', 'GO_MARIADB_AUTH_PORT': str(port),
                'GO_MARIADB_AUTH_TLS_CA': str(private/'tls-cert.pem'),
                'GO_MARIADB_AUTH_PAM_PASSWORD': metadata['password'], 'GO_MARIADB_AUTH_PAM_CODE': metadata['code'],
                'GO_MARIADB_AUTH_GSSAPI_PRINCIPAL': metadata['principal'],
                'GO_MARIADB_AUTH_GSSAPI_OTHER_CACHE': 'FILE:'+str(private/'other.ccache'),
                'KRB5CCNAME': 'FILE:'+str(private/'driver.ccache'),
                'KRB5_CONFIG': str(private/'krb5.conf')}
        finally:
            if container:
                # Exact generated name only. Never prune unrelated containers,
                # images, volumes, builder caches or the developer's databases.
                cleanup_container(container)
            if process:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def main():
    """Run a command with private fixture variables (use -- before arguments)."""
    import sys
    args = sys.argv[1:]
    local = bool(args and args[0] == '--local')
    if local:
        args = args[1:]
    if args and args[0] == '--':
        args = args[1:]
    if not args:
        raise SystemExit('usage: python3 validation/auth_fixture.py -- COMMAND [ARGUMENT ...]')
    with environment(local=local) as fixture:
        return subprocess.call(args, env={**os.environ, **fixture})


if __name__ == '__main__':
    raise SystemExit(main())
