"""The check commands whose successful receipts may be published.

Keep collection and verification on the same contract. Stage names and exit
codes alone cannot establish that race detection, vet or policy checks ran.
"""

CHECK_ENVIRONMENT = {'GOMAXPROCS': '2', 'GOFLAGS': '', 'GOWORK': 'off'}
CORRECTNESS_COMMANDS = {
    'correctness': ('go', 'test', '-race', '-json', '-count=1', '-timeout=240s',
                    '-p=1', '-parallel=2', '-tags=mariadb_validation', './...'),
    'recorder': ('python3', '-m', 'unittest', 'discover', '-s', 'validation', '-p', 'test_*.py'),
    'vet': ('go', 'vet', './...'),
}


def verified_checks(capture):
    """Return report claims only for the complete, successful check contract."""
    if capture.get('method') != 'correctness only, no benchmarks':
        raise ValueError('Expected a correctness-only capture.')
    stages = capture.get('stages', {})
    if set(stages) != set(CORRECTNESS_COMMANDS):
        raise ValueError('Required correctness stages are missing or unexpected.')
    if capture.get('check_environment') != CHECK_ENVIRONMENT:
        raise ValueError('Unexpected or missing correctness check environment.')
    for name, command in CORRECTNESS_COMMANDS.items():
        stage = stages[name]
        argv = stage.get('command', [])
        if not isinstance(argv, list) or not all(isinstance(arg, str) for arg in argv):
            raise ValueError('Required check command is missing or malformed: '+name)
        if any('bench' in arg for arg in argv):
            raise ValueError('A correctness stage contains a benchmark command.')
        if argv != list(command):
            raise ValueError('Required check command differs: '+name)
        if stage.get('exit_code') != 0 or stage.get('issues') != []:
            raise ValueError('A required correctness stage failed or is incomplete: '+name)
    return {'race': True, 'vet': True, 'harness_policy': True}
