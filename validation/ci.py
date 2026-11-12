#!/usr/bin/env python3
"""Require fixture-backed correctness; retain every raw status and test event.

Managed reuse now supplies the desired isolation contract. No failing assertion
is allowlisted. --correctness-only never executes benchmark smoke checks.
"""
import argparse
from datetime import datetime, timezone
import json
import os
import platform
import shutil
from pathlib import Path
import subprocess
import sys

from record import benchmark_rows, digest, inputs
from contracts import CHECK_ENVIRONMENT, CORRECTNESS_COMMANDS
from provenance import read_server, revision_info, server_command
import pass_history
import external_auth_evidence

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = 'github.com/aantich/go-mariadb'
MANAGED_SESSION_TEST = PACKAGE+'/validation/TestMariaDBDesiredCleanPoolReset'
REQUIRED = {PACKAGE+'/'+name for name in ('TestMariaDBMalformedGreeting', 'TestMariaDBMalformedOK',
            'TestDedicatedTerminatorValidation', 'TestDedicatedCloseKeepsCancellation', 'TestDedicatedExecCachedMetadataEOF',
            'TestDedicatedBulkCancellationPhases')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBNativeBulk', 'TestMariaDBNativeUnsignedResult',
            'TestMariaDBReviewBulkTransports', 'TestMariaDBReviewZeroTimeParity', 'TestMariaDBReviewCachedMetadataEOF',
            'TestMariaDBAuthentication/parsec', 'TestMariaDBAuthentication/ed25519',
            'TestMariaDBAuthentication/caching_sha2_password', 'TestMariaDBAuthentication/x509')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBDesiredCleanPoolReset',
            'TestMariaDBManagedSessionIsolation', 'TestMariaDBDefaultSessionPolicy',
            'TestMariaDBSessionObservations', 'TestMariaDBNativeTypeMetadata',
            'TestMariaDBManagedRestorationFailure', 'TestMariaDBManagedRoleAndSchema')}
REQUIRED |= {PACKAGE+'/'+name for name in ('TestNativeProgressPackets', 'TestNativeProgressCallbacks',
            'TestNativeStreamWire', 'TestNativeStreamBoundedStaging', 'TestNativeStreamCancellation',
            'TestNativeCursorCancellation', 'TestNativeCursorMalformedFetch', 'TestNativeBulkUnsignedUnits',
            'TestNativeOptionsAndPreflight', 'TestNativeBulkUnitMalformed', 'TestNativeNoReplay',
            'TestNativeValuerPanicCleanup')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBBulkUnits', 'TestMariaDBStreamParameters',
            'TestMariaDBServerCursors', 'TestMariaDBProgressExecution', 'TestMariaDBCursorManagedGenerations')}
REQUIRED |= {PACKAGE+'/'+name for name in ('TestNativeCursorMaterializationError',
            'TestNativeProgressCallbackBadConn', 'TestNativeStreamPacketBoundary')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBCursorMaterializationError',
            'TestMariaDBStreamPacketBoundary')}
REQUIRED |= {PACKAGE+'/'+name for name in ('TestNativeStatementReuse', 'TestNativeStatementLifetime',
            'TestNativeStatementResetAndValidation', 'TestNativeStatementCanceledWait', 'TestNativeStatementNoReplay',
            'TestNativeStatementCancellation', 'TestNativeStatementOrdinaryPreparation', 'TestNativeStatementCloseTimeout')}
REQUIRED |= {PACKAGE+'/'+name for name in ('TestNativeStatementDeferredClose', 'TestNativeStatementCloseOwnerTransfer',
            'TestNativeStatementDeferredCloseCancellation')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBNativeStatementReuse',
            'TestMariaDBNativeStatementReset', 'TestMariaDBNativeStatementConcurrent', 'TestMariaDBNativeStatementCancellation')}
REQUIRED |= {PACKAGE+'/validation/'+name for name in ('TestMariaDBNativeCloseAfterReturn',
            'TestMariaDBNativeDeferredCloseCancellation', 'TestMariaDBStage4UnitValidation')}
REQUIRED |= {PACKAGE+'/'+name for name in ('TestExternalAuthPolicy', 'TestPAMDialogProtocol',
            'TestGSSAPIProtocol', 'TestExternalAuthWire', 'TestGSSAPIRequiredMethod', 'TestExternalAuthPanicCleanup',
            'TestExternalAuthErrorSecrecy')}
REQUIRED |= {PACKAGE+'/validation/TestMariaDBExternalAuthentication',
             PACKAGE+'/gssapi/TestNativeContextCleanup', PACKAGE+'/gssapi/TestProviderCancellation',
             PACKAGE+'/gssapi/TestProviderAdmission', PACKAGE+'/gssapi/TestProviderCapacity',
             PACKAGE+'/gssapi/TestProviderDefaultAdmission'}


def assess_tests(output, exit_code, require_instrumentation=False, required=None):
    records = {}
    failed_packages = set()
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get('Action') == 'fail' and not event.get('Test'):
            failed_packages.add(event.get('Package'))
        if event.get('Test'):
            name = event['Package']+'/'+event['Test']
            record = records.setdefault(name, {'output':''})
            record['output'] += event.get('Output','')
            if event['Action'] in ('pass','fail','skip'):
                record['status'] = event['Action']
    issues = []
    required_tests = REQUIRED if required is None else required
    for name in required_tests:
        if records.get(name, {}).get('status') != 'pass':
            issues.append('Required test missing/not passing: '+name)
    if require_instrumentation and records.get(PACKAGE+'/validation/TestMariaDBSessionInstrumentation', {}).get('status') != 'pass':
        issues.append('Required isolated statement instrumentation did not pass.')
    for name, record in records.items():
        status = record.get('status')
        if status == 'fail':
            issues.append('Unexpected failure: '+name)
        profile_skip = (not require_instrumentation and name == PACKAGE+'/validation/TestMariaDBSessionInstrumentation'
                        and 'PROFILE: performance_schema requires the isolated instrumentation fixture' in record['output'])
        if status == 'skip' and name.startswith(PACKAGE+'/validation/') and not profile_skip:
            issues.append('Required MariaDB fixture skipped: '+name)
        elif status == 'skip' and any(name == test or name.startswith(test+'/') for test in required_tests):
            issues.append('Required fault test skipped: '+name)
        if status not in ('pass','fail','skip'):
            issues.append('Test did not finish: '+name)
    if 'WARNING: DATA RACE' in output:
        issues.append('Race detector reported a data race.')
    if exit_code and not any(r.get('status') == 'fail' for r in records.values()):
        issues.append('Go failed without a completed failing assertion (build/infrastructure failure).')
    for package in failed_packages:
        if not any(name.startswith(package+'/') and r.get('status') == 'fail' for name,r in records.items()):
            issues.append('Package failed without an attributable assertion: '+str(package))
    if exit_code not in (0,1):
        issues.append('Unexpected Go process exit: '+str(exit_code))
    return sorted(issues), records


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--correctness-only', action='store_true', help='Never execute benchmark functions, including smoke checks.')
    parser.add_argument('--output', type=Path, default=ROOT/'validation/results/ci')
    args = parser.parse_args()
    path = os.environ.get('MARIADB_VALIDATION_CONFIG')
    if not path:
        raise SystemExit('CI requires MARIADB_VALIDATION_CONFIG; missing fixtures must not become successful skips.')
    config = json.loads(Path(path).read_text())
    if config['database'] != 'go_mariadb_validation' or config['upstream_database'] != 'go_mariadb_upstream':
        raise SystemExit('Refusing non-reserved CI schemas.')
    if not config.get('password'):
        raise SystemExit('Private fixture credentials are required.')
    env = os.environ.copy()
    env.update(MYSQL_TEST_USER=config['user'], MYSQL_TEST_PASS=config['password'], MYSQL_TEST_DBNAME=config['upstream_database'],
               MYSQL_TEST_ADDR=f"{config['host']}:{config['port']}")
    env.update(CHECK_ENVIRONMENT)
    env.pop('MYSQL_TEST_CONCURRENT', None) # bounded-pool tests run; no global saturation
    target = args.output
    if (target/'capture.json').exists():
        raise SystemExit('Retain earlier attempts; use a new output directory.')
    target.mkdir(parents=True, exist_ok=True)
    source = inputs()
    capture = {'stages':{}, 'inputs':source, 'input_fingerprint':digest(json.dumps(source, sort_keys=True).encode()),
               'recorded_at':datetime.now(timezone.utc).isoformat(),
               **revision_info(ROOT), 'check_environment':dict(CHECK_ENVIRONMENT),
               'capabilities':json.loads((ROOT/'validation/capabilities.json').read_text()),
               'instrumentation_required':env.get('GO_MARIADB_REQUIRE_INSTRUMENTATION') == '1',
               'go':subprocess.check_output(['go','version'], env=env, text=True).strip(), 'host':platform.platform(),
               'method':'correctness only, no benchmarks' if args.correctness_only else 'correctness plus one-iteration benchmark smoke; not accepted performance measurements'}
    if env.get('GO_MARIADB_AUTH_RECEIPT'):
        capture['external_auth'] = json.loads(env['GO_MARIADB_AUTH_RECEIPT'])
    client = shutil.which('mariadb') or shutil.which('mysql')
    if not client:
        raise SystemExit('MariaDB administrative client is required for read-only provenance.')
    endpoint = {key:config[key] for key in ('host','port','user','database')}
    server_argv = server_command(endpoint, client)
    capture['server'] = {'endpoint':endpoint, 'command':server_argv}
    def server_snapshot(phase):
        try:
            snapshot, output = read_server(config, env, server_argv)
        except (ValueError, OSError, subprocess.TimeoutExpired):
            # Retain a failed attempt without echoing private subprocess state.
            # Such a receipt is deliberately ineligible for publication.
            capture['server']['error'] = 'Could not read '+phase+' server provenance.'
            capture['inputs_stable'] = source == inputs()
            capture['logs'] = {p.name:digest(p.read_bytes()) for p in target.glob('*.log')}
            capture['exit_code'] = 1
            (target/'capture.json').write_text(json.dumps(capture, indent=2)+'\n')
            raise SystemExit(capture['server']['error']) from None
        (target/('server-'+phase+'.log')).write_text(output)
        return snapshot
    before = server_snapshot('before')
    capture['server']['before'] = before
    pass_history.preserve(capture, target, ROOT/'validation')
    commands = [(name,list(argv)) for name,argv in CORRECTNESS_COMMANDS.items()]
    if not args.correctness_only:
        commands.insert(1, ('native-benchmark-smoke',['go','test','-run=^$','-bench=^BenchmarkMariaDBNativeBulk$','-benchtime=1x','-benchmem','-count=1','./validation']))
    for name, command in commands:
        result = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, timeout=300)
        output = (result.stdout+result.stderr).replace(config['password'], '[redacted]')
        (target/(name+'.log')).write_text(output)
        stage = {'command':command, 'exit_code':result.returncode, 'issues':[]}
        if name == 'correctness':
            stage['issues'], stage['cases'] = assess_tests(output, result.returncode, env.get('GO_MARIADB_REQUIRE_INSTRUMENTATION') == '1')
            try:
                external_auth_evidence.validate(capture, output)
            except (ValueError, KeyError, TypeError) as error:
                stage['issues'].append(str(error))
        elif name == 'native-benchmark-smoke':
            expected = {f'BenchmarkMariaDBNativeBulk/{rows}/{method}' for rows in (1,16,100,1000)
                        for method in ('prepared','native_bulk','text_batch','local_infile')}
            stage['profiles'] = benchmark_rows(output)
            if result.returncode or set(stage['profiles']) != expected:
                stage['issues'] = ['Native benchmark smoke did not execute all 16 alternatives.']
        elif result.returncode:
            stage['issues'] = [name+' failed.']
        capture['stages'][name] = stage
        print(name+': raw exit='+str(result.returncode)+'; unexpected issues='+str(len(stage['issues'])), flush=True)
        for issue in stage['issues']:
            print(issue)
        if stage['issues']:
            print(output, flush=True)
    after = server_snapshot('after')
    capture['server'].update(after=after, stable=before == after)
    capture['inputs_stable'] = source == inputs()
    capture['logs'] = {path.name:digest(path.read_bytes()) for path in target.glob('*.log')}
    capture['lost_correctness_passes'] = pass_history.lost(capture, target, pass_history.statuses(capture))
    capture['exit_code'] = int(bool(not capture['inputs_stable'] or not capture['server']['stable'] or capture['lost_correctness_passes'] or any(s['issues'] for s in capture['stages'].values())))
    (target/'capture.json').write_text(json.dumps(capture, indent=2)+'\n')
    return capture['exit_code']


if __name__ == '__main__':
    sys.exit(main())
