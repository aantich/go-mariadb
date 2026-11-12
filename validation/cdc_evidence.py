"""Require the current CDC fixture contract in metadata and raw Go events."""
import json


def validate(capture, output):
    receipt = capture.get('cdc_fixture')
    observed = []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get('Test') == 'TestMariaDBCDC' and 'CDC_FIXTURE ' in event.get('Output', ''):
            observed.append(json.loads(event['Output'].split('CDC_FIXTURE ', 1)[1].strip()))
    if receipt is None or observed != [receipt]:
        raise ValueError('CDC fixture provenance is missing or differs from raw events.')
    expected = {'contract', 'server_version', 'server_id', 'transport', 'binlog_format',
                'checksum', 'semisync_available', 'fixture_sha256'}
    if (set(receipt) != expected or receipt['contract'] != 'cdc-v1' or
            'MariaDB' not in receipt['server_version'] or receipt['server_id'] != 621129 or
            receipt['transport'] != 'private-unix-socket' or receipt['binlog_format'] != 'ROW' or
            receipt['checksum'] != 'CRC32' or receipt['semisync_available'] is not True or
            receipt['fixture_sha256'] != capture['inputs']['workload'].get('validation/cdc_fixture.py')):
        raise ValueError('CDC fixture contract/source does not match the tested implementation.')
