# Native APIs: bulk units, streamed parameters, cursors and progress

12 November 2026. Anton approved these four APIs after publication of the session
foundations (`b33f66b`). Work uses the existing `master` checkout. Anton later
authorized committing and pushing the completed stage and its review fixes.
The initial validation below was correctness/race/static
only. Anton subsequently authorized the [benchmark follow-up](STAGE3_BENCHMARKS.md);
that later receipt is separate. No baseline advancement or package release is included.
The subsequent [review fixes](STAGE3_REVIEW_FIXES.md) correct all six findings
and provide newer exact-source correctness evidence; the numbers below describe
the initial implementation receipt.

| API | Contract and implementation | Important limit |
| --- | --- | --- |
| `WithBulkUnitResults` / `BulkExecUnits` | Negotiate both native bulk flags; set SEND_UNIT_RESULTS on a bounded transient prepared batch; validate binary Id/Affected_rows schema and complete response; return owned uint64 counters. | Server units are not per-input error records. Partial effects are possible; no RETURNING/multiple results or implicit fallback/replay. |
| `ExecStream` / `StreamParam` | Validate all arguments before uploading; send separate bounded LONG_DATA chunks, including an empty chunk for a non-NULL empty value; reset unexecuted uploads on reader failure and retire on uncertain cleanup. | Client staging is bounded. The server still limits complete parameters. Readers remain caller-owned, one-shot and responsible for cancellation of blocking Read. |
| `WithServerCursors` | Standard database/sql.Rows and existing binary/typed decoders over read-only execute/fetch commands; explicit batch/status validation; early close drains only the current batch and resets the statement. | SELECT-only pool, no multiple results. MariaDB materializes server cursors; row-count bounds are not byte bounds or a general speed improvement. |
| `WithProgressReports` / `ExecWithProgress` | Negotiate reports explicitly; validate/route small control packets iteratively; own message strings; suppress callbacks after first error/panic but drain SQL response, preserving server errors. | Reports are advisory, synchronous and non-reentrant. Callback failure does not cancel or roll back SQL. No global server settings change. |

Native helpers keep raw access entirely inside `sql.Conn.Raw`. Context watchers
cover preparation, execution and cleanup; partially executed writes and consumed
sources never expose database/sql's replay marker. A reader/Valuer panic retires
before statement cleanup can wait on a transport. Default ordinary query/Exec
semantics remain unchanged unless the caller selects a native option.
Application callback errors are joined after raw driver error classification,
so even an application-supplied ErrBadConn remains inspectable without discarding
a terminal SQL error or retiring an otherwise healthy connection.

Cursor metadata has one cursor-open terminator on both legacy and deprecated-EOF
peers. Fetch EOFs either announce another full batch or LAST_ROW_SENT; malformed
counts/statuses retire the stream. Empty cursors, exact fetch multiples, early
close before FETCH, reusable prepared handles, DDL reprepare and managed reset
generations are covered. Go 1.27 retains RowsColumnScanner; earlier supported Go
versions use the existing driver.Value scanner.

The source comparison uses the local MariaDB server checkout, particularly
`include/mysql_com.h`, `sql/sql_prepare.cc`, `sql/sql_class.cc`,
`sql/protocol.cc` and `sql/sql_cursor.cc`. Live validation uses MariaDB 12.3.3;
the checkout's later development version is not relabeled as the runtime.
Protocol references: [bulk execute](https://mariadb.com/docs/server/reference/clientserver-protocol/3-binary-protocol-prepared-statements/com_stmt_bulk_execute),
[long data](https://mariadb.com/docs/server/reference/clientserver-protocol/3-binary-protocol-prepared-statements/com_stmt_send_long_data),
[fetch](https://mariadb.com/docs/server/reference/clientserver-protocol/3-binary-protocol-prepared-statements/com_stmt_fetch)
and [progress](https://mariadb.com/docs/server/reference/product-development/mariadb-internals/using-mariadb-with-your-programs-api/progress-reporting).

## Validation and evidence

Final input fingerprint:
`b0fcfc8e76e78454c198168f8ebc2de60e7fc53830265597ff0cb019f02817b8`.
All **51 production/module inputs** and **67 workload/CI inputs** match across
Mac and Linux and remained stable during each run. Both use Go 1.27.4 and
MariaDB 12.3.3; the receipts retain exact host strings and before/after server
snapshots. Each full run uses race detection, GOMAXPROCS=2 and bounded test
parallelism; all 44 Python harness/policy checks and vet pass. Actionlint 1.7.12
and `git diff --check` also pass.

| Profile | Unique passing leaves | Failures | Skips |
| --- | ---: | ---: | ---: |
| macOS arm64, shared managed service | **1,196** | **0** | 8 |
| Linux arm64, isolated instrumented server | **1,199** | **0** | 5 |

Both profiles add **126 passing leaves**. The confirmation separately protects
all 1,070 earlier Mac passes and all 1,073 earlier Linux passes; none was lost.
Skips retain the preceding stage's platform/fixture limits. No new API fixture
skips are accepted. The Linux run uses an existing local image, two CPUs, 4 GiB,
read-only source/exact dependency mounts, disabled dependency fetching and no
published host port. Its task-owned server is stopped after checking. Shared
Mac global settings remain unchanged. The broader hosted server/Go matrix was
not executed locally; Alpha/application-validation limits remain in force.

The [confirmation manifest](../validation/results/stage3-confirmation.json)
links the [Mac receipt](../validation/results/stage3-mac-final/capture.json)
and [Linux receipt](../validation/results/stage3-linux-final/capture.json),
including checksums for their raw logs. An earlier successful Mac development
receipt is retained separately; it predates the two final boundary/cleanup
regressions and does not establish final-source freshness.

New tests cover seven live transport/metadata modes, real ALTER progress with
callback error/panic and terminal SQL error, partial reader failures, transaction
rollback, unsigned counters, malformed control packets, cursor batches and
cancellation through upload/fetch/drain/reset/close. A generated 16 MiB upload
has an allocation-bound correctness check and a materializing negative control;
this does not measure throughput or elapsed-time performance.

Development checks caught and corrected a legacy cursor metadata-EOF wait. New
live assertions were also corrected to the actual server contract: explicit IDs
can produce a zero insert-ID counter, and a DDL error may precede any report.
Generated unsigned IDs and report-then-server-error joining have independent
tests. Final review removed copies for ignored/suppressed progress messages and
fixed overflow in the one-byte stream-limit probe at MaxInt64. A blocked-peer
Valuer-panic regression confirms retirement precedes statement cleanup writes.

The [live report](../validation/RESULTS.md) identifies exact current correctness
and its measurement source. Earlier receipts and frozen workloads remain
intact. Interactive PAM, GSSAPI and optional transaction-aware CDC remain the
three unsupported API areas; PAM/Kerberos service fixtures remain unavailable.
Performance acceptance, prepare/bulk handle reuse, pipelining, broader runtime
profiles and application pilots remain work on the [roadmap](ROADMAP.md).
The pre-benchmark timing capture is preserved separately at
`validation/results/stage3-prebenchmark-history/current.json`, SHA-256
`5fa0ebf51e2ddc51b555ff1d1e5aab5e28a976247c1419f079d95c778d0e0b4c`.
All immutable reference checksums and original workload/contract migrations pass
`validation/verify.py`; no historical measurement is relabeled as a new sample.

See the [user guide](USER_GUIDE.md) for configuration and examples.
