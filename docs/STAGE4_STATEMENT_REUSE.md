# Stage 4: reusable native statements and measurement preparation

**Review follow-up:** [All four corrections](STAGE4_REVIEW_FIXES.md) are now
implemented, with newer matching-source correctness of 1,304 Mac / 1,307 Linux
passes and zero failures. The initial implementation/receipt below is preserved
historically. New benchmark profiles remain unmeasured.

**Publication follow-up:** Anton subsequently authorized commit and push of the
complete stage and review corrections to `master`. The receipts below retain
their original prepublication context; publication does not repeat measurements.

12 November 2026. Implemented on `master`, parent `187fd7d`, and left uncommitted
for review. Anton requested implementation while the Mac was busy and explicitly
excluded benchmarks. **No benchmark function or profiling workload was executed.**
The [live report](../validation/RESULTS.md) separates new correctness evidence
from the unchanged Stage 3 timing capture. The final
[confirmation receipt](../validation/results/stage4-confirmation.json) records
source hashes, checks, preserved passes and unchanged timing/reference checksums.

## Reuse a server statement explicitly

`PrepareNative(ctx, conn, query)` returns a `*NativeStmt` with `BulkExec`,
`BulkExecUnits`, `ExecStream` and `Close` methods. The
[guide](USER_GUIDE.md#reuse-a-native-prepared-statement) gives an application
example. Standard `database/sql` remains the connection and statement owner;
applications can continue using the existing one-shot helpers.

The retained handle removes one PREPARE exchange and one CLOSE command from
each subsequent successful execution. Protocol tests assert one preparation and
one close across three calls for all three methods. This is a structural command
reduction, **not a measured throughput or allocation result**. Each batch still
undergoes conversion, bounds/type validation and encoding. Types are sent on every
execution, including after NULL, signedness and representation changes. Parameter
signature caching and prepare/execute pipelining remain separate work.

The wrapper owns a normal `*sql.Stmt` and an opaque key. A connection-local map
resolves that key only inside `Conn.Raw`; no raw driver pointer escapes into the
public wrapper. Ordinary statement preparation does not allocate this map.
Driver cleanup unregisters the handle even after connection retirement. This
keeps `database/sql`'s serialized statement cleanup effective when the application
returns its `sql.Conn` before closing the native handle.

Operations on the same handle serialize through a context-aware gate. Canceling
a waiter does not cancel another operation. Once wire work starts, the existing
cancellation/retirement and no-automatic-replay rules apply. Reader and Valuer
panics retire the connection before propagating. Methods must not be reentered
from a reader or Valuer. `Close` is idempotent, leaves the connection open and
waits for the handle's current operation. Like `sql.Stmt.Close`, it has no context;
configure `WriteTimeout` to bound stalled cleanup and cancel active work through
its execution context before waiting for close.

Managed session reset invalidates IDs using the existing generation counter.
The next operation reparses lazily and checks parameter arity before sending an
execution. Stale close cannot close an ID recycled for another statement. Server
DDL reprepare retains existing semantics. A reader error resets unexecuted upload
state so the same handle can accept a fresh source; failed reset retires it.
Native execution does not add implicit transactions, batch splitting or retries.
Bulk units remain owned `uint64` counters and may represent server execution
units rather than one record for every input row.

## Additive benchmark workloads, still unmeasured

The original 200 profiles and both references are unchanged. The new
[workload source](../validation/stage4_bench_test.go) and
[required-profile catalog](../validation/stage4_workloads.py) add **62 profiles**:

| Family | Alternatives | Profiles / units |
| --- | --- | --- |
| Bulk | aggregate and unit helpers, each one-shot/reused; ordinary prepared loop | 20; 1/16/100/1,000 rows per transaction |
| Upload | ordinary byte-slice one-shot/reused, reused native bytes, reader one-shot/reused | 15; 64 KiB/1 MiB/16 MiB per transaction |
| Cursor | ordinary prepared versus fetch sizes 1/16/128/1,024; typed/borrowed scans | 20; 1,000/10,000 rows consumed and checked |
| Progress DDL | disabled, negotiated/discarded, callback and callback error | 4; two ALTER COPY commands plus data verification |
| Quiet progress | disabled, negotiated/discarded and callback | 3; one DO command without reports |

Bulk/stream writes use durable InnoDB tables in the reserved fixture schema.
Every alternative times BEGIN, DELETE, execution, COMMIT and content checks.
Retained preparation/close are outside timing; one-shot preparation/close stay
inside. Bulk input matrices are prebuilt. Uploads generate fresh identical data
on each iteration; byte alternatives materialize it, readers generate bounded
chunks. Generation and server length/digest verification are timed. These are
application-level alternatives, not isolated codec measurements or peak retained
heap measurements. Cursor profiles measure local fetch tradeoffs, not WAN latency;
MariaDB still materializes server cursors. Progress callback cases require real
reports and reject silent fixture disappearance.

The full collector/verifier requires all 262 profiles with seven repetitions
and validates the new operation units. An authorized first complete run can use
`--freeze-stage4-baseline`; it refuses correctness-only collection and any existing
Stage 4 reference. No such reference exists yet. Subsequent runs compare the same
names and immutable workload hashes. See the [run instructions](../validation/README.md).

## Read-path investigation: evidence still needed

The earlier text narrow/BLOB timing signals remain unresolved. Source inspection
of [direct scanning](../rows_column_scanner.go) confirms that owned `[]byte` and
`any` destinations clone packet-backed data, while `sql.RawBytes` borrows it.
Text numeric destinations require checked conversion. Removing those ownership
copies or bounds checks would change correctness contracts and cannot be justified
by the old timings alone. Borrowed BLOB scans also showed a slowdown in the
earlier capture, so ownership copying alone is not an established explanation.

[profile_reads.py](../validation/profile_reads.py) prepares exact original
text-narrow and owned/borrowed text-BLOB CPU/heap commands. Its default is dry-run:
no subprocess, database access, credentials or output directory. Execution needs
explicit `--execute` and observed `--conditions` after profiling is authorized.
It retains source and endpoint provenance in a separate diagnostic directory and
never publishes timing pointers. Instrumentation overhead makes those profiles
diagnostic, not accepted performance measurements. No read decoder/scanner fast
path was changed in this stage. Actual attribution, candidate optimization and
matched timing confirmation await a suitable measurement window.

## Correctness and remaining work

Persistent tests cover native command reuse and cleanup, canceled waits, phase
cancellation, no replay, panic retirement, SQL/conversion errors, arity changes,
recycled statement IDs and ordinary preparation overhead isolation. Live tests
cover seven transport/metadata modes, type/NULL/signedness changes, counters above
`MaxInt64`, reader-error recovery, exact rollback, DDL, multiple managed reset
generations and concurrent calls. The confirmation receipt records full
race-enabled Mac/Linux checks, harness tests and vet; no benchmark smoke is part
of that contract.

Final executable-input fingerprint:
`4d2c8a294bd0495610075aced928a321daf12aab7e44cd048703a04e618e147c`
(52 production/module files and 77 workload/CI inputs).

| Final correctness profile | PASS | FAIL | SKIP | Earlier passes protected | New passes |
| --- | ---: | ---: | ---: | ---: | ---: |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,260 | 0 | 8 | 1,222 | 38 |
| Isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,263 | 0 | 5 | 1,225 | 38 |

Both final receipts pass the full race/tag/package contract, vet and all **62
harness checks**, with matching source fingerprints and stable before/after
server settings. Formatting, diff checks and actionlint pass. The Linux fixture
uses two CPUs, 4 GiB, read-only source and existing public module caches; it
publishes no ports, fetches no dependencies and was removed after checks. Mac
skips include shared-server instrumentation; real isolated instrumentation passes
on Linux. Other inherited skips are named in the confirmation. Older Go/server
versions and hosted CI execution are not established by these local results.
Earlier Stage 4 receipts are retained; final confirmations followed a public
API comment correction. The 200-profile timing pointer remains byte-identical
(`02fea12c…adea7ed`), and all immutable reference checksums verify.

Next measurement work is to collect the new profiles, diagnose the existing
text-read signals and assess matched before/after performance. Pipelining,
interactive PAM/GSSAPI with real service fixtures, optional CDC, wider
server/platform runs and application pilots remain on the [roadmap](ROADMAP.md).
Readiness stays Alpha; no commit, push or package release was made in this pass.
