"""Shared completeness checks for recorded benchmark evidence.

References define mandatory profile identities. A profile disappearing in every
process is a missing measurement, even when all remaining profiles have seven
samples. Keep this check shared by publication and later freshness verification.
"""
import math
import statistics


def benchmark_issues(benchmarks, references, repetitions=7):
    required = {}
    required_metrics = {}
    for reference in references:
        for name, row in reference['benchmarks'].items():
            required[name] = row['samples'][0]['family']
            required_metrics[name] = set(row['median'])-{'iterations','gomaxprocs'}
    issues = ['Missing benchmark profile: '+name for name in sorted(required.keys()-benchmarks.keys())]
    if not benchmarks:
        issues.append('No benchmark measurements.')
    for name, row in benchmarks.items():
        samples = row.get('samples', [])
        metrics = required_metrics.get(name, set()) | {'ns/op','B/op','allocs/op'}
        family = required.get(name, samples[0].get('family') if samples else None)
        identities = [(s.get('family'), s.get('repetition')) for s in samples]
        if family not in ('upstream','mariadb') or len(samples) != repetitions or set(identities) != {(family, r) for r in range(repetitions)}:
            issues.append('Missing/duplicate/wrong-family repetitions: '+name)
        for sample in samples:
            iterations = sample.get('iterations')
            if sample.get('gomaxprocs') != 4 or not isinstance(iterations, int) or isinstance(iterations, bool) or iterations <= 0:
                issues.append('Invalid benchmark process/iteration metadata: '+name)
            for metric in metrics:
                value = sample.get(metric)
                if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value < 0 or (metric == 'ns/op' and value == 0):
                    issues.append('Invalid/missing '+metric+': '+name)
        for metric in metrics:
            values = [s.get(metric) for s in samples]
            if values and all(isinstance(v, (int, float)) and math.isfinite(v) for v in values):
                if row.get('median', {}).get(metric) != statistics.median(values):
                    issues.append('Incorrect/missing median '+metric+': '+name)
    return sorted(set(issues))
