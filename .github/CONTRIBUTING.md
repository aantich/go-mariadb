# Contributing to Go-MariaDB

This is an independent MariaDB driver, forked from
[go-sql-driver/mysql](https://github.com/go-sql-driver/mysql). MariaDB is the
supported development target; shared protocol names are retained where needed.
The project is Alpha. See the [roadmap](../docs/ROADMAP.md) before starting a
larger feature and discuss public API/lifetime changes in this repository.

Check this repository's existing issues before opening a new report. Include a
minimal reproduction, exact source revision, MariaDB/Go/platform versions,
connection mode and relevant server settings. Remove passwords, tokens and
private keys from examples and logs.

Contributions use MPL-2.0, as specified in LICENSE. Preserve existing copyright
and attribution, and add yourself or the copyright holder to AUTHORS.

Use idiomatic Go, gofmt and useful public API comments. Explain ownership,
resource bounds, context cancellation, error outcomes and borrowed-data lifetimes
where relevant. Add regressions that reproduce the actual issue rather than
mirroring the implementation.

Run the [validation workflow](../validation/README.md) against a real MariaDB
before integration/performance measurement. Use only reserved test schemas and
accounts; the suite must not alter application data or shared global settings.
Unit/race/vet, parser fuzzing and live tests serve different purposes. Missing
fixtures and unimplemented APIs must remain explicit.

Preserve the original baseline, workload bodies and raw repetitions. Add new
feature tests and benchmarks in new files. Compare equivalent contents,
transaction boundaries, result verification and environments; record allocations
and timing separately. Do not change a workload or select favorable repetitions
to obtain an apparent speedup. Use record.py/verify.py for current captures.

Submit focused changes with a clear behavior description, validation evidence,
limitations and any performance tradeoff. Do not publish a package/tag or change
an accepted benchmark reference as a side effect of a fix.
