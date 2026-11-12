"""Protect correctness leaves independently of when timings were last measured.

Each new attempt snapshots its newest successful matching-profile receipt.
Historical code need not be fresh: its checked pass identities are the reference.
Raw events and checksums must corroborate that receipt before it is selected.
"""
import gzip
import json
from pathlib import Path

from contracts import FULL_COMMANDS, verified_checks

CONTRACT = 'correctness-history-v1'
FILENAME = 'prior-correctness.json'


def profile(capture):
    server = capture.get('server_provenance', capture.get('server', {}))
    snapshot = server.get('before', server)
    result = {'go': capture.get('go'), 'host': capture.get('host'),
              'server_version': snapshot.get('version'),
              'instrumentation_required': bool(capture.get('instrumentation_required', False))}
    if any(not isinstance(result[key], str) or not result[key] for key in ('go', 'host', 'server_version')):
        raise ValueError('Missing correctness history profile.')
    return result


def statuses(capture):
    import record
    stages = capture.get('stages', {})
    if 'correctness' in stages:
        cases = stages['correctness'].get('cases', {})
        return {record.case_key(name): case.get('status') for name, case in cases.items()}
    return {record.case_key(case['name']): case.get('status')
            for name in ('upstream', 'protocol', 'mariadb')
            for case in stages.get(name, {}).get('cases', [])}


def passing_leaves(capture):
    cases = statuses(capture)
    parents = set()
    for name in cases:
        parent = name.rpartition('/')[0]
        while parent:
            parents.add(parent)
            parent = parent.rpartition('/')[0]
    return {name for name, status in cases.items() if status == 'pass' and name not in parents}


def eligible(capture):
    if capture.get('exit_code') != 0 or capture.get('inputs_stable') is not True:
        return False
    stages = capture.get('stages', {})
    if capture.get('method') == 'correctness only, no benchmarks':
        try:
            verified_checks(capture)
        except ValueError:
            return False
        return True
    return (capture.get('complete') is True and all(
        stages.get(name, {}).get('command') == list(command) and stages[name].get('exit_code') == 0
        for name, command in FULL_COMMANDS.items()))


def checked_reference(capture, directory):
    """Authenticate historical test identities without claiming source freshness."""
    import ci
    import record
    from provenance import validate_server
    current_statuses = statuses(capture)
    if not eligible(capture) or not passing_leaves(capture):
        raise ValueError('Prior correctness receipt is not successful or has no passing leaves.')
    if capture.get('input_fingerprint') != record.digest(json.dumps(capture['inputs'], sort_keys=True).encode()):
        raise ValueError('Prior correctness source fingerprint is inconsistent.')
    for name, expected in capture.get('logs', {}).items():
        if Path(name).name != name or record.digest((directory/name).read_bytes()) != expected:
            raise ValueError('Prior correctness log checksum mismatch: '+name)
    if 'correctness' in capture['stages']:
        required = {'correctness.log', 'recorder.log', 'vet.log', 'server-before.log', 'server-after.log'}
        if set(capture.get('logs', {})) != required:
            raise ValueError('Prior correctness logs are incomplete.')
        issues, observed = ci.assess_tests((directory/'correctness.log').read_text(), 0,
                                         capture.get('instrumentation_required', False), required=())
        if issues or observed != capture['stages']['correctness']['cases']:
            raise ValueError('Prior correctness raw events differ or failed.')
        validate_server(capture, directory)
    else:
        for name in ('upstream', 'protocol', 'mariadb', 'race'):
            log = name+'.log.gz'
            if log not in capture.get('logs', {}):
                raise ValueError('Prior full-run correctness log is missing: '+log)
            text = gzip.decompress((directory/log).read_bytes()).decode()
            issues, _ = ci.assess_tests(text, 0, required=())
            if issues or record.test_cases(text) != capture['stages'][name]['cases']:
                raise ValueError('Prior full-run correctness events differ or failed.')
            if name != 'race':
                current_statuses.update({case['name']:case.get('status') for case in record.test_cases(text, include_containers=True)})
        validate_server({'server': capture['server_provenance'],
                         'instrumentation_required': capture.get('instrumentation_required', False)}, directory)
    if capture.get('pass_history_contract') is not None:
        validate(capture, directory, current_statuses)


def newest(capture, here):
    """Skip other runtime/fixture profiles; validate the selected receipt strictly."""
    wanted = profile(capture)
    candidates = []
    pointer = here/'results/correctness-current.json'
    paths = [*here.joinpath('results').rglob('capture.json')]
    if pointer.exists():
        paths.append(pointer)
    for path in paths:
        try:
            candidate = json.loads(path.read_text())
            if path == pointer and not eligible(candidate):
                raise ValueError('Latest correctness pointer is not successful.')
            if eligible(candidate) and profile(candidate) == wanted:
                recorded = candidate.get('recorded_at')
                if not isinstance(recorded, str) or not recorded:
                    raise ValueError('Missing historical correctness date.')
                candidates.append((recorded, path, candidate))
        except (ValueError, KeyError):
            if path == pointer:
                raise ValueError('Latest correctness pointer is unreadable.') from None
    if not candidates:
        return None, None
    _, path, reference = max(candidates, key=lambda item: (item[0], str(item[1])))
    directory = path.parent
    if reference.get('evidence_directory'):
        directory = (here/reference['evidence_directory']).resolve()
        if not directory.is_relative_to(here.resolve()):
            raise ValueError('Prior correctness evidence is outside validation.')
    checked_reference(reference, directory)
    return reference, str(path.relative_to(here))


def write_snapshot(capture, directory, reference=None, source=None):
    """Also used by synthetic policy fixtures; never executes tests or SQL."""
    import record
    snapshot = {'profile': profile(capture), 'reference': reference, 'source': source}
    data = (json.dumps(snapshot, indent=2)+'\n').encode()
    (directory/FILENAME).write_bytes(data)
    capture['pass_history_contract'] = CONTRACT
    capture['prior_correctness'] = {'file': FILENAME, 'sha256': record.digest(data)}
    capture['lost_correctness_passes'] = []


def preserve(capture, directory, here):
    reference, source = newest(capture, here)
    write_snapshot(capture, directory, reference, source)


def lost(capture, directory, observed):
    import record
    if capture.get('pass_history_contract') != CONTRACT:
        raise ValueError('Missing correctness history contract.')
    meta = capture.get('prior_correctness', {})
    if meta.get('file') != FILENAME:
        raise ValueError('Missing prior correctness snapshot.')
    data = (directory/FILENAME).read_bytes()
    if record.digest(data) != meta.get('sha256'):
        raise ValueError('Prior correctness snapshot checksum mismatch.')
    snapshot = json.loads(data)
    if snapshot.get('profile') != profile(capture):
        raise ValueError('Prior correctness profile differs from this attempt.')
    reference = snapshot.get('reference')
    if reference is None:
        if snapshot.get('source') is not None:
            raise ValueError('First correctness profile has an inconsistent source.')
        return [] # Explicit first run for this compiler/host/server/fixture profile.
    if not eligible(reference) or profile(reference) != profile(capture) or not passing_leaves(reference):
        raise ValueError('Prior correctness snapshot is incomplete or mismatched.')
    normalized = {record.case_key(name): status for name, status in observed.items()}
    return sorted(name for name in passing_leaves(reference) if normalized.get(name) != 'pass')


def validate(capture, directory, observed):
    missing = lost(capture, directory, observed)
    if missing or capture.get('lost_correctness_passes') != []:
        raise ValueError('Prior correctness passing leaves lost: '+', '.join(missing))
