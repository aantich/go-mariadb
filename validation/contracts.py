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

# Timed workloads keep GOMAXPROCS=4 to match the immutable Mac references.
# Compilation flags and external workspaces must not silently change the code
# that either the tests or the separately built benchmark binaries execute.
FULL_CHECK_ENVIRONMENT = {'GOMAXPROCS': '4', 'GOFLAGS': '', 'GOWORK': 'off'}
FULL_COMMANDS = {
    'upstream': ('go', 'test', '-json', '-count=1', '-timeout=240s', '.', './zstd', './internal/...', './gssapi'),
    'protocol': ('go', 'test', '-json', '-count=1', '-timeout=60s', '-tags=mariadb_validation', '-run=^TestMariaDB', '.'),
    'mariadb': ('go', 'test', '-json', '-count=1', '-timeout=120s', './validation'),
    # Require the complete tagged suite with race detection, including protocol
    # fault tests; ordinary counts still come from the three preceding commands.
    'race': CORRECTNESS_COMMANDS['correctness'],
    'vet': CORRECTNESS_COMMANDS['vet'],
    'recorder': CORRECTNESS_COMMANDS['recorder'],
    'fuzz': ('go', 'test', '-run=^$', '-fuzz=^FuzzDedicatedLengthEncoding$', '-fuzztime=8s', '-parallel=4', '.'),
    'fuzz-terminator': ('go', 'test', '-run=^$', '-fuzz=^FuzzDedicatedTerminator$', '-fuzztime=8s', '-parallel=4', '.'),
}


def measurement_commands(directory):
    """The binaries that are checked and timed belong to this exact run."""
    commands = dict(FULL_COMMANDS)
    for family, package in (('upstream', '.'), ('mariadb', './validation')):
        binary = str(directory/(family+'.test'))
        commands['build-'+family] = ('go', 'test', '-c', '-o', binary, package)
        for repetition in range(7):
            commands[f'bench-{family}-{repetition}'] = (binary, '-test.run=^$', '-test.bench=.',
                '-test.benchtime=200ms', '-test.benchmem', '-test.count=1', '-test.timeout=240s')
    return commands


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
