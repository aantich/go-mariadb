# Stage 7: MariaDB compatibility and failure recovery

Authorized 12 November 2026, following published Stage 6 commit `ec42783d`.
Anton selected version compatibility and failure recovery. Work stays on `master`
and remains uncommitted for review. Readiness stays **Alpha**; benchmarks,
benchmark smoke runs and profiling remain deferred.

## Implementation scope

1. Add a reproducible, isolated correctness matrix for MariaDB
   **10.11 / 11.4 / 11.8 / 12.3**. Resolve images to immutable digests, record the
   actual patch version, compiler, host, inputs, server settings and raw logs.
   Use owned temporary datadirs, credentials and TLS; never change the shared
   service. Preserve failed attempts and prevent missing cases from passing.
2. Exercise inherited SQL/protocol/unit checks plus dedicated version-aware
   native API and CDC coverage. Assert successful supported behavior and explicit
   rejection for unavailable capabilities. Do not weaken the complete 12.3 suite
   or turn unsupported server features into silent passing skips.
3. Add deterministic failure recovery: lock timeout, deadlock victim/survivor,
   connection loss before/partway through a write, lost success responses,
   uncertain COMMIT, explicit rollback and pool recovery. Assert effects through
   an independent observer. Automatic retries must never replay a write whose
   execution is possible. Include native operations and CDC durable resume.
4. Add mandatory CI coverage for the supported server lines and Go
   **1.25 / 1.26 / 1.27**. Keep full current-version authentication/CDC/instrumentation
   validation separately required. Record actual local coverage, including any
   compiler/platform limits, rather than claiming an unrun Cartesian matrix.
5. Review and fix discovered driver/fixture issues. Repeat full exact-source
   Mac and isolated Linux correctness/race/vet/harness checks, update the live
   report and publish the new compatibility evidence alongside historical results.

No automatic SQL transaction retry API, failover router, XA recovery, new server
feature emulation or package release is introduced. Application retry decisions
after an uncertain commit require application-level reconciliation/idempotency.

## Server feature boundaries

Capability negotiation remains authoritative. The version matrix checks expected
differences; it does not select a server backend or infer a negotiated capability
solely from a version string.

* Bulk unit results were added in MariaDB 11.5.1; older lines must reject their
  explicit request without changing rows. [Protocol differences](https://mariadb.com/docs/server/reference/clientserver-protocol/mariadb-protocol-differences-with-mysql).
* PARSEC is available from MariaDB 11.6; older lines remain covered for supported
  native-password/ed25519 authentication. [PARSEC documentation](https://mariadb.com/docs/server/reference/plugins/authentication-plugins/authentication-plugin-parsec).
* VECTOR was introduced in 11.7, with general availability in 11.8 LTS; tests on
  10.11/11.4 assert its absence instead of attributing it to driver failure.
  [MariaDB Vector](https://mariadb.org/projects/mariadb-vector/).
* Lock timeout and deadlock are different outcomes. A lock timeout normally rolls
  back the statement; deadlock rolls back the selected transaction. Tests inspect
  both outcomes and explicitly roll back unfinished work.
  [InnoDB variables](https://mariadb.com/docs/server/server-usage/storage-engines/innodb/innodb-system-variables).

## Implemented behavior and fixes

`validation/compatibility.py` runs inherited SQL/protocol/CDC unit tests, dedicated
cross-version integration tests and vet, with race detection. The declared
four-server/three-compiler CI matrix is separate from the mandatory full current
authentication/CDC/instrumentation suite. Every image is pinned by digest; actual
patch versions, binary identity, compiler checksum, raw events, source inputs,
server snapshots and resource cleanup are recorded. Earlier matching-profile
passing leaves are independently protected, including later-added assertions.

The new native-policy cases cover aggregate/reused bulk, reader parameters,
server cursors, owned scanned values, progress reports, extended metadata,
session reset and lazy statement generations over plain/zlib/TLS, plus legacy
EOF and disabled metadata-cache controls. Unit-result requests on older servers
must return `ErrUnsupportedCapability` without changing rows. VECTOR and plugin
availability have explicit positive/negative server-profile assertions.

Real recovery cases create lock timeout and a deadlock cycle without scheduling
sleeps, then verify rollback/survivor effects through an independent observer.
Fault injection operates on actual TCP commands: no bytes sent, partial command,
and a complete server OK consumed then withheld from the driver. Ordinary
autocommit's safe pre-write retry is distinguished from uncertain writes; native
bulk/stream and COMMIT cases verify no unsafe replay, physical retirement and
subsequent pool recovery. Restart preserves committed rows, rolls back open work
and exercises existing prepared handles on fresh physical connections.

CDC cases kill the private source connection after a complete persisted/ACKed
group, verify progress does not change on failure, then reopen by GTID across
DDL and consume exactly the later row group. Both plain and compressed verified
TLS paths are covered. The matrix's server and the full suite's separate CDC
fixture are owned; shared service globals remain untouched.

The matrix exposed a driver compatibility bug: an empty GTID list can contain
two zero padding bytes. MariaDB deliberately reserves six body bytes so older
replicas can substitute a dummy event. The decoder accepts exactly that padded
form and compact summaries, while rejecting malformed/nonempty trailing data.
Errors now retain their cause and identify event type/position without dumping
source SQL or row data. Attributed source excerpts, the original failing 10.11
receipt and strict positive/malformed/fuzz regressions are preserved.

Fixture checks also found and corrected Darwin's unreaped zombie-leader probe,
Docker's changing ephemeral port on restart and a read-only-checkout assumption
in a history test. Failed starts cannot read, restart or delete an unowned
container; volume cleanup remains independent. Controller failures preserve
redacted output and terminate their owned child group. No earlier failure was
deleted or converted into a skip.

## Final current-version correctness

Source `98ba80ed279200ff93c51231f27cd9568f56f8fe8d67b7b7335f079f0526635f`
(68 driver/native files, 109 test/harness/CI inputs).

| Profile | Passing leaves | Failures | Skips | Race / vet / harness |
| --- | ---: | ---: | ---: | --- |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,637 | 0 | 8 | PASS / PASS / 99 PASS |
| isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,640 | 0 | 5 | PASS / PASS / 99 PASS |

All 1,588/1,591 Stage 6 passing leaves survive, with **49 added passes per
profile**. The 8/5 inherited/profile skips remain unchanged; no required fixture
or new recovery check skipped. The Linux checkout is read-only and requires real
instrumentation and PAM/KDC/native provider coverage.

The [compatibility matrix](../validation/COMPATIBILITY.md) records its separate
per-profile counts and exact compiler/server/platform scope. These narrower
older-server profiles do not replace the complete current-version suite.

Two compiled, race-enabled defect restorations fail the intended tests: rejecting
empty-list padding and returning a replay marker after a completed write loses
its OK. The ten new harness tests reject omitted cases/history/markers, changed
sources/commands/runtime facts, forged cleanup and foreign-resource access.
Bounded CDC fuzz campaigns, CGO-disabled tests, Windows amd64 root/CDC and Linux
386 CDC cross-compilation, actionlint and format/diff checks pass. Cross-builds
do not establish Windows/386 runtime coverage. No remote run of the new CI matrix
is claimed before this uncommitted stage is published.

## Evidence and reproduction

* [Live current-version correctness](../validation/RESULTS.md)
* [Exact compatibility profiles](../validation/COMPATIBILITY.md)
* [Final confirmation and hashes](../validation/results/stage7-confirmation.json)
* [Mac receipt](../validation/results/stage7-mac-final-3/capture.json)
* [Linux receipt](../validation/results/stage7-linux-final-3/capture.json)
* [Development failures](../validation/results/stage7-development-20261112)
  and [controls/toolchain/source evidence](../validation/results/stage7-20261112)

Run one version profile with:

```sh
python3 validation/compatibility.py --family 10.11 \
  --go /absolute/path/to/go --output validation/results/stage7-unique-attempt
```

Use each of `10.11`, `11.4`, `11.8`, `12.3` with the intended Go compiler.
Docker, OpenSSL and a MariaDB CLI are required. Each output directory must be new.
The full current-version command remains in the
[validation guide](../validation/README.md). Do not run benchmarks or profiles
until Anton authorizes a measurement window.

All captures are correctness evidence. Historical benchmark/reference files
remain byte-identical; no timing reference was advanced and no CDC speedup is
claimed. Earlier source attempts are preserved, including collector/harness
corrections and source changes detected mid-run. Final publication uses only
complete successful exact-source receipts. Stage 7 stays uncommitted on `master`.

## Remaining scope

This is an Alpha compatibility snapshot, not production certification. Local
matrix clients are macOS arm64 with Linux arm64 Docker servers; the new CI matrix
targets Linux runners, and the full current-version Linux profile is separately
executed. Broader OS/architecture/runtime deployments, real applications,
multi-node failover and production identity systems remain validation work.
XA/two-phase ALTER, engine-specific binlog modes and live MariaDB 13.2 partial
rows retain their earlier explicit limits. Pipelining and measured read-path
optimization remain deferred pending an authorized benchmark/profile window.
