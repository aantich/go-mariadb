# MariaDB validation for the Go fork

## Current dedicated driver

The [review corrections](../docs/STAGE1_REVIEW_FIXES.md) add persistent protocol,
cleanup, native bulk transport/date/fault regressions and strict benchmark
completeness guards. The full report is generated, including its overview,
performance signals and workload-specific units. Do not manually repair captions
or summary numbers after each capture.

The driver now has its own module/package/SQL registration. Use:

```sh
python3 -m unittest discover -s validation -p 'test_*.py'
python3 validation/record.py --conditions 'Describe observed host conditions'
python3 validation/verify.py
```

`--correctness-only` runs tests/race/vet/recorder/fuzz checks without replacing
the complete live benchmark report. `--freeze-native-baseline` is reserved for
the first complete matched bulk capture and refuses an existing reference.
The original baseline is immutable. Exact inverse identity substitutions verify
original workload bodies; every original passing test remains protected.
All tracked and untracked production Go files are fingerprinted. Native helpers
add sixteen matched bulk profiles and independent first measurements.

The original desired-default-clean-pool test still fails: reset/restoration is
an open API gap. Full capture exit 1 honestly retains that failure. Current
readiness is Alpha; inspect the live report rather than treating a checksum pass
as correctness success. See [implementation](../docs/DEDICATED_DRIVER.md) and
[user guide](../docs/USER_GUIDE.md).

The additional `mariadb-validation` CI job runs fixture-backed Linux MariaDB
12.3 on Go 1.25/1.26/1.27. Missing live fixtures and required test skips fail the
job. It retains the exact known clean-session failure as a warning/raw failing
assertion; no additional failures are allowed. Hosted-runner native benchmarks
execute all sixteen paths once as smoke checks, without publishing comparable
timings. Local equivalent: set `MARIADB_VALIDATION_CONFIG` and run
`python3 validation/ci.py`. Isolated Linux certificate/plugin provisioning uses
`prepare_ci.py`; it must not be used to reconfigure the shared Mac service.

## Original unchanged-driver workflow (historical)

This suite records the actual inherited driver before changes, then provides
matching comparisons for future stages. It does not modify production Go code,
change global MariaDB settings or substitute a simulator for live-server tests.
Deterministic peer mocks supplement the real server for malformed-protocol cases.

## Run in this order

1. Install and start a real MariaDB service. This first profile uses the existing
   managed Homebrew MariaDB 12.3.3 service on macOS, Go 1.27.4 and Apple M4 Max.
   Source review of current MariaDB main is recorded separately.
2. Verify socket administration works as your local OS account:
   `mariadb --protocol=socket -e 'SELECT VERSION()'`.
3. Provision only the reserved Go fixtures:

   ```sh
   python3 validation/provision.py
   ```

   The script creates `go_mariadb_validation`, `go_mariadb_upstream` and
   `gomdb_*` accounts. The ignored owner-only `validation/.local/config.json`
   holds credentials. TLS paths initially reuse the existing local validation
   CA/client certificate files; the Haskell schemas/accounts/results are untouched.
   Review/edit the script for another environment before provisioning. It does
   not install plugins, change root authentication or modify global configuration.
4. For the **first capture only**, while the host is idle:

   ```sh
   python3 -m unittest discover -s validation -p 'test_*.py'
   python3 validation/run.py --freeze-baseline --conditions 'Describe the actual host conditions'
   ```

   For later matching captures, omit `--freeze-baseline`. The runner refuses to
   overwrite the baseline, runs all stages, retains failing assertions and
   updates `RESULTS.md` and `results/current.json` after complete measurement.
   An exit of 1 is expected for this unchanged-driver baseline because real
   findings remain. It is not a clean bill of health.
5. Verify evidence freshness and baseline file hashes:

   ```sh
   python3 validation/check.py
   ```

   Freshness is separate from correctness. A fresh capture can correctly contain
   failures. No command commits, pushes, changes an accepted reference or publishes
   a package.

## Stages and evidence

* **Inherited suite:** original tests for the root driver, zstd subpackage and
  internal packages run against the separate upstream fixture schema. All
  inherited test files are unchanged. Original skips and their output remain.
* **Protocol suite:** `-tags=mariadb_validation -run '^TestMariaDB'` adds internal
  malformed greeting/OK/header/column/cache and receive-limit assertions. A
  recovered panic records a failing test; it is never an expected PASS.
* **MariaDB suite:** live types/parameters, SQL features, results, transactions,
  TLS/authentication, cancellation, ownership, pool concurrency and LOCAL INFILE.
* **Race:** repeats inherited and live cases with `-race`. Assertion failures
  are separate from race-detector warnings. The malformed-packet suite has no
  concurrent mutable fixture state and is recorded independently.
* **Vet and fresh benchmark builds:** compilation/static failures remain visible.
* **Performance:** all 144 inherited and 40 added benchmark profiles, seven
  process repetitions per family, `GOMAXPROCS=4`, `-benchtime=200ms`, `-benchmem`.
  Serial added workloads perform two checked warmups; inherited and parallel
  workloads retain Go's calibration. Family repetition order is seeded/shuffled.

The user confirmed an idle-host window for the initial measurements.
A late CPU snapshot during the final benchmark family nevertheless showed an
active background virtualization process. The [observation](results/host-observation-20261111.json)
qualifies the current report; it is not continuous monitoring and does not prove
the size of any timing effect. Original baseline artifacts remain unchanged.
Correctness/race builds finish before benchmark processes start.

Every capture records commit, dirty-tree status, actual driver and workload
hashes, compiler, module graph, OS/architecture, server settings, commands,
exit codes, leaf test cases and all benchmark samples. Baseline logs are compressed
and checksummed. Private credentials are redacted before log writing. Local run
binaries/caches are ignored; the initial evidence is retained under `baselines/`.

## Benchmark semantics

* Added narrow/wide reads consume **1,000 rows per operation**. Every result is
  counted and checksummed; wide results verify every column. BLOB reads validate
  **1 MiB per operation**. Typed and borrowed `sql.RawBytes` scans are separate.
  Raw numeric scans include conversion needed for verification.
* Added writes replace **100 rows in one transaction per operation**. BEGIN,
  DELETE, insert(s), COMMIT and count/sum/content verification are timed for all
  alternatives: prepared loop, text batch and reader-based LOCAL INFILE. Native
  binary bulk has no implementation or fabricated timing.
* Connection cases include opening/authenticating/initializing and closing one
  connection. The TLS configuration/CA is constructed outside timing. Bounded
  parallel pool cases measure single-scalar requests with one or eight connections.
* The metadata control uses the same 32-column prepared query and scanner. A
  bounded wrapper clears only the MariaDB metadata-cache greeting bit in the
  uncached alternative, allowing the unmodified driver to negotiate normally.
  Both alternatives use the wrapper; received bytes are recorded as `wire-B/op`.
* Inherited workloads keep their own semantics: some receive 10,000 rows, and
  scanner microbenchmarks decode either one or 100 rows per operation. Names and
  original source define their units. Do not normalize all inherited ns/op into
  the new live-read 1,000-row unit.

**Baseline / Current / Change** always compares the same named workload in the
same environment. Change is `(Current / Baseline - 1) × 100`; negative means less
elapsed time. The main table uses ns/operation; capture JSON also retains ns/row,
bytes/second, bytes/operation, allocations/operation and wire bytes where available.
An API-alternative comparison is different from an old-versus-new driver claim.
Go and Haskell results are not cross-language comparisons.

## Preserve the baseline

Never edit the first baseline or initial workload bodies to make a failure pass.
Fix driver code; add new tests/benchmarks in new files and separate feature
baselines. The runner protects original passing test identities and rejects
changed frozen workload hashes or mismatched environments. New server, compiler,
hardware or network profiles need their own baseline and explicit profile support;
the current local provisioning/recorder targets this first Mac profile only.

The source review and [coverage map](COVERAGE.md) identify missing APIs and
environment gaps. This broad initial suite is not exhaustive certification of
every server version, topology, storage engine, plugin or failure timing.
