"""Bind external-auth fixture provenance to the actual test-event log."""
import json
import re


def validate(capture, output):
    receipt = capture.get('external_auth')
    observed = []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get('Test') == 'TestMariaDBExternalAuthentication' and 'EXTERNAL_AUTH ' in event.get('Output', ''):
            observed.append(json.loads(event['Output'].split('EXTERNAL_AUTH ', 1)[1].strip()))
    if receipt is None and not observed:
        # This verifier is used only by the current correctness/full-run
        # contracts. Historical receipts remain archived, not silently upgraded
        # to current evidence. Synthetic tests must supply explicit provenance.
        raise ValueError('Missing external authentication fixture provenance.')
    if receipt is None or observed != [receipt]:
        raise ValueError('External authentication fixture receipt differs from raw test events.')
    fields = {'contract', 'runtime_identity', 'server_version', 'realm', 'service_principal',
              'tls_certificate_sha256', 'fixture_inputs'}
    if set(receipt) != fields or receipt['contract'] != 'external-auth-v1':
        raise ValueError('Unexpected external authentication provenance contract.')
    if (not isinstance(receipt['runtime_identity'], str) or not re.fullmatch(
            r'docker-runtime-sha256:[a-f0-9]{64}|local-disposable-linux-profile', receipt['runtime_identity']) or
            'MariaDB' not in receipt['server_version'] or receipt['realm'] != 'GO-MARIADB.TEST' or
            receipt['service_principal'] != 'mariadb/localhost@GO-MARIADB.TEST' or
            not re.fullmatch('[a-f0-9]{64}', receipt['tls_certificate_sha256'])):
        raise ValueError('Invalid external authentication runtime identity.')
    inputs = capture['inputs']['workload']
    if set(receipt['fixture_inputs']) != {'Dockerfile', 'serve.py', 'pam_fixture.c'} or any(
            sha != inputs.get('validation/auth_fixture/'+name) for name, sha in receipt['fixture_inputs'].items()):
        raise ValueError('External authentication fixture source differs from measured source.')
