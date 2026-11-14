"""Required Stage 4 profiles and their operation units; no measured values.

Compilation is not measurement. Until a complete authorized run is frozen,
these profiles have no performance baseline. Existing 200-profile references
remain independent and immutable.
"""

STAGE4_PROFILES = {}
for rows in (1, 16, 100, 1000):
    for method in ('aggregate_once', 'aggregate_reused', 'units_once', 'units_reused', 'prepared_loop'):
        STAGE4_PROFILES[f'BenchmarkMariaDBStage4Bulk/{rows}/{method}'] = {'rows/op': rows}
for size in (65536, 1048576, 16777216):
    for method in ('bytes_once', 'bytes_reused', 'bytes_sql_stmt', 'reader_once', 'reader_reused'):
        STAGE4_PROFILES[f'BenchmarkMariaDBStage4Stream/{size}/{method}'] = {'payload-B/op': size}
for rows in (1000, 10000):
    for fetch in (0, 1, 16, 128, 1024):
        for scan in ('typed', 'borrowed'):
            STAGE4_PROFILES[f'BenchmarkMariaDBStage4Cursor/{rows}/{fetch}/{scan}'] = {'rows/op': rows}
for method in ('disabled', 'discarded', 'callback', 'callback_error'):
    STAGE4_PROFILES[f'BenchmarkMariaDBStage4Progress/{method}'] = {'commands/op': 2}
for method in ('disabled', 'discarded', 'callback'):
    STAGE4_PROFILES[f'BenchmarkMariaDBStage4ProgressQuiet/{method}'] = {'commands/op': 1}

STAGE4_WORKLOAD_INPUTS = ('validation/stage4_bench_test.go', 'validation/stage4_workloads.py')


def stage4_issues(benchmarks):
    """Require all new alternatives even before their first reference exists."""
    issues = []
    for name, units in STAGE4_PROFILES.items():
        row = benchmarks.get(name)
        if row is None:
            issues.append('Missing Stage 4 benchmark profile: '+name)
            continue
        for sample in row.get('samples', []):
            if sample.get('family') != 'mariadb':
                issues.append('Wrong Stage 4 benchmark family: '+name)
            for metric, expected in units.items():
                if sample.get(metric) != expected:
                    issues.append('Wrong/missing Stage 4 operation unit '+metric+': '+name)
        if any(row.get('median', {}).get(metric) != expected for metric, expected in units.items()):
            issues.append('Wrong/missing Stage 4 median unit: '+name)
    return sorted(set(issues))
