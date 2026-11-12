"""Verify full measurement receipts using the same policy as collection.

Raw logs corroborate stage summaries, required fixtures and benchmark samples.
Historical timing captures remain historical; only this contract can establish
fresh, publishable full-run results for newly measured source.
"""
import gzip
import json
from pathlib import Path

from contracts import FULL_CHECK_ENVIRONMENT, FULL_COMMANDS, measurement_commands
from evidence import benchmark_issues
from provenance import validate_server

CONTRACT = 'full-v1'
ORDINARY = ('upstream', 'protocol', 'mariadb')


def profile_server(snapshot):
    """Retain the original profile's names/types for honest old/new comparisons."""
    fields = ('version', 'max_allowed_packet', 'local_infile', 'log_bin', 'performance_schema',
              'innodb_flush_log_at_trx_commit', 'sync_binlog', 'sql_mode', 'time_zone', 'max_connections')
    return {**{key:str(snapshot[key]) for key in fields},
            'charset':snapshot['character_set_server'], 'collation':snapshot['collation_server']}


def validate_checks(capture, outputs):
    # Lazy imports avoid record -> full_capture -> ci -> record at module load.
    import ci
    import record
    if capture.get('validation_contract') != CONTRACT or capture.get('check_environment') != FULL_CHECK_ENVIRONMENT:
        raise ValueError('Missing or unexpected full-run check contract/environment.')
    stages = capture.get('stages', {})
    for name, command in FULL_COMMANDS.items():
        stage = stages.get(name, {})
        if stage.get('command') != list(command):
            raise ValueError('Required full-run check command differs: '+name)
        if stage.get('exit_code') != 0 or name not in outputs:
            raise ValueError('Required full-run check failed or has no raw log: '+name)
        if name in (*ORDINARY, 'race'):
            issues, _ = ci.assess_tests(outputs[name], 0, required=())
            if issues or stage.get('cases') != record.test_cases(outputs[name]):
                raise ValueError('Raw full-run test events differ or failed: '+name+'; '+', '.join(issues))
    for names in (ORDINARY, ('race',)):
        issues, _ = ci.assess_tests('\n'.join(outputs[name] for name in names), 0,
                                  capture.get('instrumentation_required', False))
        if issues:
            raise ValueError('Required full-run coverage failed: '+', '.join(issues))


def validate_provenance(capture, directory):
    snapshot = validate_server({'server':capture.get('server_provenance', {}),
                               'instrumentation_required':capture.get('instrumentation_required', False)}, directory)
    if profile_server(snapshot) != capture.get('server'):
        raise ValueError('Full-run comparison settings differ from the measured endpoint.')
    return snapshot


def prior_reference(capture, directory):
    """Preserve pass identities added since the original immutable baselines."""
    import record
    data = (directory/'prior-capture.json').read_bytes()
    if record.digest(data) != capture.get('logs', {}).get('prior-capture.json'):
        raise ValueError('Prior full-capture reference is missing or changed.')
    previous = json.loads(data)
    if previous.get('complete') is not True:
        raise ValueError('Prior full-capture reference is incomplete.')
    return previous


def validate_passes(capture, directory, outputs, references):
    import record
    previous = prior_reference(capture, directory)
    observed = {name:record.test_cases(outputs[name], include_containers=True) for name in ORDINARY}
    lost = record.lost_reference_passes([*references, previous], observed)
    if lost or capture.get('lost_baseline_passes') != []:
        raise ValueError('Prior passing assertions lost or missing audit: '+', '.join(lost))


def validate(capture, directory):
    """Reject omitted checks, raw-log drift and fabricated benchmark summaries."""
    import record
    if (capture.get('complete') is not True or capture.get('exit_code') != 0 or
            capture.get('inputs_stable') is not True or capture.get('inputs') != record.inputs()):
        raise ValueError('Full capture failed, incomplete or stale.')
    if capture.get('input_fingerprint') != record.digest(json.dumps(capture['inputs'], sort_keys=True).encode()):
        raise ValueError('Incorrect full-run source fingerprint.')
    if capture.get('capabilities') != json.loads((record.HERE/'capabilities.json').read_text()):
        raise ValueError('Capability inventory changed after measurement.')
    execution = Path(capture.get('execution_directory', ''))
    if not execution.is_absolute():
        raise ValueError('Missing full-run execution directory.')
    commands = measurement_commands(execution)
    stages = capture.get('stages', {})
    if set(stages) != set(commands):
        raise ValueError('Required full-run stages are missing or unexpected.')
    for name, command in commands.items():
        if stages[name].get('command') != list(command) or stages[name].get('exit_code') != 0:
            raise ValueError('Required full-run command differs or failed: '+name)
    logs = capture.get('logs', {})
    expected_logs = {name+'.log.gz' for name in commands} | {'server-before.log', 'server-after.log', 'prior-capture.json'}
    if set(logs) != expected_logs:
        raise ValueError('Full-run raw log set is incomplete or unexpected.')
    for name, expected in logs.items():
        if record.digest((directory/name).read_bytes()) != expected:
            raise ValueError('Full-run log checksum mismatch: '+name)
    outputs = {name:gzip.decompress((directory/(name+'.log.gz')).read_bytes()).decode() for name in commands}
    validate_checks(capture, outputs)
    validate_provenance(capture, directory)
    references = [json.loads((path/'capture.json').read_text()) for path in
                  (record.HERE/'baselines'/record.PROFILE, record.NATIVE_BASELINE) if path.exists()]
    if not references:
        raise ValueError('Missing immutable measurement reference.')
    original = references[0]
    if (not record.baseline_workloads_match(original) or
            any(capture.get(key) != original[key] for key in ('go', 'host', 'server')) or
            capture.get('hardware') != references[-1].get('hardware', ['Apple M4 Max','16','137438953472']) or
            [record.case_key(name) for name in capture.get('modules', [])] != original['modules']):
        raise ValueError('Measured endpoint/workload/environment differs from the reference profile.')
    for path in (record.HERE/'baselines').iterdir():
        if (path/'SHA256SUMS').exists() and not record.reference_hashes_match(path):
            raise ValueError('Immutable measurement reference checksum mismatch.')
    if record.NATIVE_BASELINE.exists() and not record.native_workloads_match(references[-1], capture['inputs']):
        raise ValueError('Native workload differs from its first reference.')
    validate_passes(capture, directory, outputs, references)
    actual_samples = {}
    for family in ('upstream', 'mariadb'):
        for repetition in range(7):
            name = f'bench-{family}-{repetition}'
            rows = record.benchmark_rows(outputs[name])
            if not rows or stages[name].get('profiles') != len(rows):
                raise ValueError('Raw benchmark profiles missing/differ: '+name)
            for profile, metrics in rows.items():
                actual_samples.setdefault(profile, []).append({'family':family, 'repetition':repetition, **metrics})
    benchmarks = capture.get('benchmarks', {})
    if set(actual_samples) != set(benchmarks):
        raise ValueError('Raw benchmark identities differ from receipt.')
    for name, samples in actual_samples.items():
        recorded = benchmarks[name].get('samples', [])
        if sorted(recorded, key=lambda s:(s['family'],s['repetition'])) != sorted(samples, key=lambda s:(s['family'],s['repetition'])):
            raise ValueError('Raw benchmark samples differ from receipt: '+name)
    issues = benchmark_issues(benchmarks, references)
    if issues:
        raise ValueError('\n'.join(issues))
    return {'race': True, 'vet': True, 'harness_policy': True, 'server_stable': True}
