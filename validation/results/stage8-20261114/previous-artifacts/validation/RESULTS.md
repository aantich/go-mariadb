# Go MariaDB live validation and benchmarks

## Latest correctness — native MariaDB APIs

Recorded: **2026-11-13T14:37:43.644777+00:00** · Source: `a16c00cf01a35b193ab251edbadb1a04ece062906b8a3f12bffbabd6cee449f1` · **1637 PASS / 0 FAIL / 8 SKIP**, unique leaves.
Server: **12.3.3-MariaDB-log** · Go: `go version go1.27.4 darwin/arm64` · Host: `macOS-26.4-arm64-arm-64bit-Mach-O` · GOMAXPROCS: **2** · performance_schema: **0**. Server settings match before/after checks.
Race detection, vet, harness policy checks pass. No prior passing assertion was lost. Managed isolation is explicitly opted in; ordinary checkout remains liveness-only.
[Raw correctness receipt](results/stage7-review-fixes-mac-final-2/capture.json) · [Reusable statements](../docs/STAGE4_STATEMENT_REUSE.md) · [Native API contracts](../docs/STAGE3_NATIVE_APIS.md) · [Session foundations](../docs/STAGE2_SESSION_FOUNDATIONS.md).

**No performance measurements were made for this source.** The timing tables and full-capture failures below belong to the earlier source fingerprint shown there. They are historical, qualified measurements; none were copied into a new baseline or presented as current-source timings.

<details><summary>Earlier full-capture results and historical failures</summary>

## Earlier full performance capture

> Dedicated MariaDB driver, Alpha. Baseline = unchanged upstream; Current = the measured working tree below.

Recorded: **2026-11-12T10:54:13.171129+00:00** · Parent commit: `b33f66b0d491` · Go: `go version go1.27.4 darwin/arm64`

Inputs: `d40de51ee6e045d07c242a9af50e0356a6cd5dd7e2d362570b9df7b1cc37a2a4` · Matches committed source: **False**. See capture JSON for the complete dirty tree and per-file hashes.

Server: **12.3.3-MariaDB-log** · Host: `macOS-26.4-arm64-arm-64bit-Mach-O` · GOMAXPROCS: **4**.

Benchmark conditions: **User authorized benchmarks on 12 November 2026; no explicit idle-host confirmation. Active desktop applications remain running; per-family CPU snapshots retained for all seven process repetitions. Correctness/race/fuzz and builds finish before timing. No shared-server global settings changed. Confirmation capture after fixing raw-event protection of historical parent PASS labels and the obsolete session-gap caption; driver and all Go benchmark workloads unchanged from the first 200-profile measurement.**. Seven independent processes per benchmark family; medians and raw repetitions are retained.

## Earlier measured stage at a glance

🟢 **1196 passing assertions** · 🔴 **0 failed assertions** · ⏭ **8 skips**.
Ordinary totals exclude repeated race checks. Clean session reuse is explicitly opted in; ordinary pool checkout remains liveness-only.
The complete workflow exits **0**. Readiness remains **Alpha**; automated coverage does not establish production application readiness.

All **200 benchmark profiles** require **seven independent process repetitions**. Original passing assertions lost: **0**.
**Performance acceptance remains open:** 33 original profiles exceed +5% time, 4 exceed +20%, and 7 improve by more than 5%.
These are investigation thresholds, not proof of a code-caused change. Measurement conditions and per-family host observations remain in the capture.

| Rows per transaction | Prepared loop, current ms | Native bulk, current ms | Prepared/native throughput ratio | Text batch, current ms | LOCAL INFILE, current ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.241 | 0.278 | 0.86× | 0.245 | 0.274 |
| 16 | 0.786 | 0.310 | 2.54× | 0.288 | 0.306 |
| 100 | 3.765 | 0.466 | 8.08× | 0.466 | 0.475 |
| 1000 | 35.800 | 2.861 | 12.51× | 2.674 | 2.901 |

These compare matched API alternatives on the current driver. Text/TSV data and parameter matrices are prebuilt; native encoding remains timed. Every alternative includes the same transaction, deletion and count/sum/content verification. Native bulk includes transient prepare/close; the prepared control reuses its handle. These are not universal fork-versus-upstream speedups.

The [review corrections](../docs/STAGE1_REVIEW_FIXES.md), [implementation record](../docs/DEDICATED_DRIVER.md) and [coverage limits](COVERAGE.md) describe supported paths and remaining work. Original references and earlier captures remain unchanged.

## Earlier full-capture correctness

| Suite | PASS | FAIL | SKIP | Exit |
| --- | ---: | ---: | ---: | ---: |
| upstream | 831 | 0 | 7 | 0 |
| protocol | 78 | 0 | 0 | 0 |
| mariadb | 287 | 0 | 1 | 0 |
| race | 1118 | 0 | 8 | 0 |
| vet | 0 | 0 | 0 | 0 |
| recorder | 0 | 0 | 0 | 0 |
| fuzz | 0 | 0 | 0 | 0 |
| fuzz-terminator | 0 | 0 | 0 | 0 |
| build-upstream | 0 | 0 | 0 | 0 |
| build-mariadb | 0 | 0 | 0 | 0 |
| bench-upstream-6 | 0 | 0 | 0 | 0 |
| bench-upstream-5 | 0 | 0 | 0 | 0 |
| bench-upstream-3 | 0 | 0 | 0 | 0 |
| bench-mariadb-1 | 0 | 0 | 0 | 0 |
| bench-upstream-4 | 0 | 0 | 0 | 0 |
| bench-upstream-0 | 0 | 0 | 0 | 0 |
| bench-mariadb-4 | 0 | 0 | 0 | 0 |
| bench-mariadb-0 | 0 | 0 | 0 | 0 |
| bench-mariadb-5 | 0 | 0 | 0 | 0 |
| bench-mariadb-6 | 0 | 0 | 0 | 0 |
| bench-upstream-1 | 0 | 0 | 0 | 0 |
| bench-mariadb-2 | 0 | 0 | 0 | 0 |
| bench-upstream-2 | 0 | 0 | 0 | 0 |
| bench-mariadb-3 | 0 | 0 | 0 | 0 |

Historical totals retain the parser used by that capture; latest correctness excludes all ancestor containers. Race cases repeat coverage.

## Earlier full-capture failures

| Suite | Baseline failures | Earlier capture failures |
| --- | ---: | ---: |
| upstream | 0 | 0 |
| protocol | 76 | 0 |
| mariadb | 3 | 0 |

</details>

## Current capability inventory

| Capability | Baseline | Current | Evidence / next step |
| --- | --- | --- | --- |
| reusable native statements | new inventory | SUPPORTED | PrepareNative bulk/unit/stream handle reuse; database/sql-owned cleanup, reset generations, fresh reader recovery, type changes, concurrency and phase faults. Throughput/allocation effects remain unmeasured. |
| MariaDB metadata caching | SUPPORTED | SUPPORTED | packets.go:initCapabilities; validation metadata cached/uncached wire-byte control |
| ed25519 authentication | SUPPORTED | SUPPORTED | auth_ed25519.go; real ed25519 account |
| Unix/TLS/X.509 authentication | SUPPORTED | SUPPORTED | real socket identity and CA/hostname/client-certificate tests |
| large []byte parameters | SUPPORTED | SUPPORTED | existing COM_STMT_SEND_LONG_DATA; 2 MiB value/checksum checks |
| LOCAL INFILE reader | SUPPORTED | SUPPORTED | RegisterReaderHandler; real data/count/sum verification |
| Go 1.27 direct typed row scanning | SUPPORTED | SUPPORTED | rows_column_scanner.go; inherited RowsScan and typed/RawBytes benchmarks |
| native binary bulk execution | UNSUPPORTED_API | SUPPORTED | Opt-in BulkExec; real 1/16/100/1000-row tests, unsigned/NULL/DEFAULT/IGNORE, errors and validation-before-send. Aggregate results only. |
| per-unit bulk results | UNSUPPORTED_API | SUPPORTED | WithBulkUnitResults and BulkExecUnits; bounded owned uint64 Id/Affected_rows units, live insert/update/rollback/error/transport checks. No per-row error records or RETURNING contract. |
| extended type metadata | UNSUPPORTED_API | SUPPORTED | Opt-in WithExtendedMetadata; real UUID/INET4/INET6/JSON attributes, Describe and ColumnTypes, caching/DDL/reset and owned bounded parsing. MariaDB 12.3 exposes VECTOR as VARBINARY without a type-name attribute. |
| session-state observation | UNSUPPORTED_API | SUPPORTED | Opt-in WithSessionTracking and pinned InspectSession; bounded OK/terminator blocks and owned last-observed variables/schema, including legacy EOF, errors and reset generations. |
| clean connection reset/restoration | UNSUPPORTED_API | SUPPORTED | Opt-in WithSessionReset and pinned ResetSession; charset/role/schema/ordered params/InitSQL restoration, rollback, private-state cleanup, lazy statement generations, cancellation and retirement. Default checkout remains liveness-only. |
| server cursor fetch | UNSUPPORTED_API | SUPPORTED | WithServerCursors; ordinary database/sql.Rows over read-only COM_STMT_FETCH batches, metadata caching/legacy EOF, early close/reset and cancellation faults. Server materializes SELECT cursors; multiple result sets unsupported. |
| io.Reader prepared parameters | UNSUPPORTED_API | SUPPORTED | ExecStream and StreamParam; chunk-bounded one-shot upload, exact limits/empty/NULL/reader errors, live transport and transaction tests, cancellation phases and bounded allocation with materialization control. Server still bounds complete parameters. |
| progress callbacks | UNSUPPORTED_API | SUPPORTED | WithProgressReports and ExecWithProgress; owned validated reports, real ALTER COPY callbacks on all transport profiles, callback error/panic draining and server-error preservation; no report guarantee or global server-setting changes. |
| PARSEC authentication | UNSUPPORTED_API | SUPPORTED | Built-in parsec plugin; real MariaDB account succeeds, binary challenge/signature and malformed/work-factor unit checks. |
| interactive PAM authentication | UNSUPPORTED_API | SUPPORTED | Opt-in PAM dialogue policy, verified TLS/Unix transport, prompt/response budgets and cancellation; isolated real single/multiple-question and rejection fixtures. See docs/STAGE5_AUTHENTICATION.md. |
| Kerberos GSSAPI authentication | UNSUPPORTED_API | SUPPORTED | Explicit principal-pinned mutual authentication and optional native macOS/Linux provider, Kerberos-only credentials and bounded asynchronous cleanup; isolated real KDC/MariaDB tests. Windows SSPI/NTLM is not implemented. |
| transaction-aware CDC/GTID resume | UNSUPPORTED_API | SUPPORTED | Optional cdc package: dedicated bounded pull stream, owned row images, complete-group checkpoints, MariaDB domain GTID resume and explicit semisync receipts. Private real binlog/TLS/zlib/compression/rotation/restart fixtures and protocol faults; XA, two-phase ALTER and engine-specific replication modes fail closed. See docs/STAGE6_CDC.md. |
| performance_schema instrumentation | ENVIRONMENT | SUPPORTED | Required real statement/thread/connection-attribute checks on isolated Linux MariaDB with performance_schema=ON. Shared Mac service stays OFF and reports a profile skip. |
| PAM/Kerberos services | ENVIRONMENT | SUPPORTED | Task-owned disposable PAM/MariaDB/KDC services, synthetic PAM credentials, real tickets and private caches/CA; fixture source/runtime provenance recorded. No host realm/PAM changes or production identity-system claim. |

Inventory entries are source-reviewed API gaps, not passing feature tests. Missing external services are recorded separately from missing APIs. See [coverage and limits](COVERAGE.md).

## Benchmark comparison

Last measured source: `d40de51ee6e045d07c242a9af50e0356a6cd5dd7e2d362570b9df7b1cc37a2a4`. Current source has **no performance measurements**.

**Stage 4: 62 new profiles are UNMEASURED.** No benchmark or profiling workload ran during this implementation. Their first reference is pending an authorized measurement window; the 200-profile historical capture below is unchanged. See [workload units and limits](../docs/STAGE4_STATEMENT_REUSE.md#additive-benchmark-workloads-still-unmeasured).

Baseline = immutable unchanged-upstream capture for original workloads; [first native-bulk capture](baselines/native-bulk-stage1-macos-arm64-mariadb-12.3.3-go1.27.4/capture.json) for the new matched bulk profiles. Current = latest complete capture. Change = `(Current / Baseline - 1) × 100`; negative means less time. Units are **ns/operation**. The original added live reads consume 1,000 rows, BLOB reads consume 1 MiB, and original added writes replace 100 rows transactionally. Native bulk alternatives use **1/16/100/1000 rows per transaction**. Inherited workloads retain their own row counts and decoder/scanner units; do not apply the live-read row count to every inherited benchmark. Connection operations establish and close one connection.

When present, the independent Stage 4 reference is `baselines/native-stage4-macos-arm64-mariadb-12.3.3-go1.27.4/capture.json`. Stage 4 bulk uses 1/16/100/1,000 rows, uploads 64 KiB/1 MiB/16 MiB, cursor reads 1,000/10,000 rows, ALTER progress two DDL commands plus verification, and quiet progress one command per operation. See the operation metrics in raw samples.

| Benchmark | Baseline ns/op | Last measured ns/op | Change vs baseline | Last measured B/op | Last measured allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| `BenchmarkExec` | 7,838.0 | 8,033.0 | +2.5% | 154 | 8 |
| `BenchmarkExecContext/1` | 10,338.0 | 9,575.0 | -7.4% | 152 | 8 |
| `BenchmarkExecContext/2` | 8,028.0 | 7,968.0 | -0.7% | 152 | 8 |
| `BenchmarkExecContext/3` | 7,531.0 | 7,493.0 | -0.5% | 152 | 8 |
| `BenchmarkExecContext/4` | 7,167.0 | 6,952.0 | -3.0% | 152 | 8 |
| `BenchmarkInterpolation` | 221.8 | 218.4 | -1.5% | 176 | 1 |
| `BenchmarkMariaDBConnect/socket` | 76,531.0 | 71,887.0 | -6.1% | 6,260 | 35 |
| `BenchmarkMariaDBConnect/text` | 141,123.0 | 144,192.0 | +2.2% | 6,509 | 43 |
| `BenchmarkMariaDBConnect/tls` | 1,174,989.0 | 1,132,473.0 | -3.6% | 86,000 | 541 |
| `BenchmarkMariaDBMetadata/cached` | 31,077.0 | 31,802.0 | +2.3% | 1,840 | 14 |
| `BenchmarkMariaDBMetadata/uncached` | 33,527.0 | 33,365.0 | -0.5% | 3,888 | 15 |
| `BenchmarkMariaDBNativeBulk/1/local_infile` | 267,344.0 | 274,217.0 | +2.6% | 21,208 | 57 |
| `BenchmarkMariaDBNativeBulk/1/native_bulk` | 262,800.0 | 278,331.0 | +5.9% | 2,392 | 50 |
| `BenchmarkMariaDBNativeBulk/1/prepared` | 231,833.0 | 240,534.0 | +3.8% | 2,392 | 50 |
| `BenchmarkMariaDBNativeBulk/1/text_batch` | 245,576.0 | 244,717.0 | -0.3% | 2,696 | 54 |
| `BenchmarkMariaDBNativeBulk/100/local_infile` | 460,474.0 | 475,456.0 | +3.3% | 21,216 | 58 |
| `BenchmarkMariaDBNativeBulk/100/native_bulk` | 455,482.0 | 466,158.0 | +2.3% | 8,704 | 53 |
| `BenchmarkMariaDBNativeBulk/100/prepared` | 3,622,989.0 | 3,764,597.0 | +3.9% | 19,829 | 546 |
| `BenchmarkMariaDBNativeBulk/100/text_batch` | 458,859.0 | 465,731.0 | +1.5% | 2,704 | 55 |
| `BenchmarkMariaDBNativeBulk/1000/local_infile` | 2,798,302.0 | 2,901,144.0 | +3.7% | 21,220 | 58 |
| `BenchmarkMariaDBNativeBulk/1000/native_bulk` | 2,870,575.0 | 2,861,121.0 | -0.3% | 61,748 | 53 |
| `BenchmarkMariaDBNativeBulk/1000/prepared` | 34,907,736.0 | 35,800,451.0 | +2.6% | 178,285 | 5,046 |
| `BenchmarkMariaDBNativeBulk/1000/text_batch` | 2,668,387.0 | 2,674,222.0 | +0.2% | 2,708 | 55 |
| `BenchmarkMariaDBNativeBulk/16/local_infile` | 307,305.0 | 305,628.0 | -0.5% | 21,216 | 58 |
| `BenchmarkMariaDBNativeBulk/16/native_bulk` | 297,750.0 | 309,569.0 | +4.0% | 3,248 | 52 |
| `BenchmarkMariaDBNativeBulk/16/prepared` | 789,377.0 | 785,559.0 | -0.5% | 5,041 | 126 |
| `BenchmarkMariaDBNativeBulk/16/text_batch` | 275,142.0 | 287,742.0 | +4.6% | 2,704 | 55 |
| `BenchmarkMariaDBPool/connections_1` | 31,564.0 | 32,326.0 | +2.4% | 680 | 20 |
| `BenchmarkMariaDBPool/connections_8` | 10,870.0 | 9,595.0 | -11.7% | 536 | 17 |
| `BenchmarkMariaDBRead/compressed/blob/raw=false` | 5,107,048.0 | 5,054,917.0 | -1.0% | 1,060,879 | 26 |
| `BenchmarkMariaDBRead/compressed/blob/raw=true` | 5,296,447.0 | 5,377,398.0 | +1.5% | 648 | 22 |
| `BenchmarkMariaDBRead/compressed/narrow/raw=false` | 1,200,599.0 | 1,125,522.0 | -6.3% | 26,802 | 2,966 |
| `BenchmarkMariaDBRead/compressed/narrow/raw=true` | 1,269,046.0 | 1,225,468.0 | -3.4% | 15,512 | 1,712 |
| `BenchmarkMariaDBRead/compressed/wide/raw=false` | 1,882,802.0 | 1,839,213.0 | -2.3% | 3,904 | 68 |
| `BenchmarkMariaDBRead/compressed/wide/raw=true` | 1,905,227.0 | 1,847,228.0 | -3.0% | 197,238 | 23,913 |
| `BenchmarkMariaDBRead/prepared/blob/raw=false` | 2,434,221.0 | 2,692,031.0 | +10.6% | 2,105,854 | 19 |
| `BenchmarkMariaDBRead/prepared/blob/raw=true` | 2,314,439.0 | 2,590,518.0 | +11.9% | 1,057,264 | 17 |
| `BenchmarkMariaDBRead/prepared/narrow/raw=false` | 226,600.0 | 233,303.0 | +3.0% | 24,048 | 2,917 |
| `BenchmarkMariaDBRead/prepared/narrow/raw=true` | 241,120.0 | 245,387.0 | +1.8% | 12,992 | 1,663 |
| `BenchmarkMariaDBRead/prepared/wide/raw=false` | 496,080.0 | 504,628.0 | +1.7% | 2,608 | 17 |
| `BenchmarkMariaDBRead/prepared/wide/raw=true` | 1,317,961.0 | 1,337,151.0 | +1.5% | 194,216 | 23,862 |
| `BenchmarkMariaDBRead/socket/blob/raw=false` | 3,304,196.0 | 3,352,469.0 | +1.5% | 2,105,901 | 19 |
| `BenchmarkMariaDBRead/socket/blob/raw=true` | 3,251,866.0 | 3,286,776.0 | +1.1% | 1,057,312 | 17 |
| `BenchmarkMariaDBRead/socket/narrow/raw=false` | 282,040.0 | 290,032.0 | +2.8% | 36,680 | 4,564 |
| `BenchmarkMariaDBRead/socket/narrow/raw=true` | 247,567.0 | 252,465.0 | +2.0% | 13,088 | 1,664 |
| `BenchmarkMariaDBRead/socket/wide/raw=false` | 2,349,018.0 | 2,377,935.0 | +1.2% | 402,121 | 52,720 |
| `BenchmarkMariaDBRead/socket/wide/raw=true` | 1,472,006.0 | 1,535,089.0 | +4.3% | 196,320 | 23,893 |
| `BenchmarkMariaDBRead/text/blob/raw=false` | 2,389,052.0 | 3,023,003.0 | +26.5% | 2,105,902 | 19 |
| `BenchmarkMariaDBRead/text/blob/raw=true` | 2,269,412.0 | 3,020,386.0 | +33.1% | 1,057,313 | 17 |
| `BenchmarkMariaDBRead/text/narrow/raw=false` | 512,291.0 | 656,345.0 | +28.1% | 36,680 | 4,564 |
| `BenchmarkMariaDBRead/text/narrow/raw=true` | 536,490.0 | 587,596.0 | +9.5% | 13,088 | 1,664 |
| `BenchmarkMariaDBRead/text/wide/raw=false` | 2,715,388.0 | 2,718,831.0 | +0.1% | 402,122 | 52,720 |
| `BenchmarkMariaDBRead/text/wide/raw=true` | 2,035,901.0 | 1,914,711.0 | -6.0% | 196,320 | 23,893 |
| `BenchmarkMariaDBRead/tls/blob/raw=false` | 2,695,418.0 | 2,682,462.0 | -0.5% | 2,106,348 | 23 |
| `BenchmarkMariaDBRead/tls/blob/raw=true` | 2,707,999.0 | 2,653,529.0 | -2.0% | 1,057,514 | 18 |
| `BenchmarkMariaDBRead/tls/narrow/raw=false` | 341,167.0 | 355,535.0 | +4.2% | 36,686 | 4,564 |
| `BenchmarkMariaDBRead/tls/narrow/raw=true` | 267,864.0 | 272,434.0 | +1.7% | 13,090 | 1,664 |
| `BenchmarkMariaDBRead/tls/wide/raw=false` | 2,402,114.0 | 2,576,210.0 | +7.2% | 402,249 | 52,720 |
| `BenchmarkMariaDBRead/tls/wide/raw=true` | 1,693,686.0 | 1,838,115.0 | +8.5% | 196,722 | 23,893 |
| `BenchmarkMariaDBWrite/load_data_reader_100` | 451,136.0 | 437,864.0 | -2.9% | 19,841 | 47 |
| `BenchmarkMariaDBWrite/sequential_prepared_100` | 3,606,958.0 | 3,676,201.0 | +1.9% | 23,102 | 843 |
| `BenchmarkMariaDBWrite/text_batch_100` | 444,333.0 | 437,976.0 | -1.4% | 1,376 | 44 |
| `BenchmarkParseDSN` | 6,146.0 | 6,318.0 | +2.8% | 17,258 | 130 |
| `BenchmarkQuery` | 10,459.0 | 10,709.0 | +2.4% | 571 | 19 |
| `BenchmarkQueryCompressed` | 10,496.0 | 10,571.0 | +0.7% | 579 | 19 |
| `BenchmarkQueryContext/1` | 11,385.0 | 10,137.0 | -11.0% | 552 | 18 |
| `BenchmarkQueryContext/2` | 8,507.0 | 8,409.0 | -1.2% | 552 | 18 |
| `BenchmarkQueryContext/3` | 7,763.0 | 7,853.0 | +1.2% | 552 | 18 |
| `BenchmarkQueryContext/4` | 7,415.0 | 7,269.0 | -2.0% | 552 | 18 |
| `BenchmarkQueryRawBytes/size=100` | 1,116,598.0 | 1,417,927.0 | +27.0% | 3,219 | 119 |
| `BenchmarkQueryRawBytes/size=1000` | 1,063,091.0 | 1,113,739.0 | +4.8% | 3,241 | 122 |
| `BenchmarkQueryRawBytes/size=12000` | 1,452,578.0 | 1,454,414.0 | +0.1% | 3,329 | 122 |
| `BenchmarkQueryRawBytes/size=16000` | 1,511,778.0 | 1,647,647.0 | +9.0% | 3,364 | 122 |
| `BenchmarkQueryRawBytes/size=2000` | 1,151,954.0 | 1,169,492.0 | +1.5% | 3,241 | 122 |
| `BenchmarkQueryRawBytes/size=256000` | 4,152,874.0 | 3,920,332.0 | -5.6% | 7,926 | 122 |
| `BenchmarkQueryRawBytes/size=32000` | 1,870,201.0 | 1,863,878.0 | -0.3% | 3,517 | 122 |
| `BenchmarkQueryRawBytes/size=4000` | 1,191,269.0 | 1,232,614.0 | +3.5% | 3,241 | 122 |
| `BenchmarkQueryRawBytes/size=64000` | 2,192,162.0 | 2,141,593.0 | -2.3% | 3,938 | 122 |
| `BenchmarkQueryRawBytes/size=8000` | 1,374,434.0 | 1,368,559.0 | -0.4% | 3,292 | 122 |
| `BenchmarkReceive10kRows/query` | 2,172,409.0 | 2,175,787.0 | +0.2% | 624 | 18 |
| `BenchmarkReceive10kRowsCompressed/query` | 6,099,741.0 | 5,957,324.0 | -2.3% | 75,243 | 2,247 |
| `BenchmarkReceiveMetadata/query` | 864,221.0 | 885,542.0 | +2.5% | 41,405 | 14 |
| `BenchmarkRoundtripBin` | 37,003.0 | 37,610.0 | +1.6% | 597 | 17 |
| `BenchmarkRoundtripTxt` | 47,253.0 | 47,259.0 | +0.0% | 9,749 | 16 |
| `BenchmarkRowsNext/binary=false/columns=1/value=123456789` | 56.9 | 60.6 | +6.5% | 8 | 1 |
| `BenchmarkRowsNext/binary=false/columns=1/value=42` | 42.7 | 45.2 | +5.8% | 0 | 0 |
| `BenchmarkRowsNext/binary=false/columns=1/value=blob` | 101.7 | 97.8 | -3.8% | 24 | 1 |
| `BenchmarkRowsNext/binary=false/columns=32/value=123456789` | 1,069.0 | 1,122.0 | +5.0% | 256 | 32 |
| `BenchmarkRowsNext/binary=false/columns=32/value=42` | 611.8 | 672.9 | +10.0% | 0 | 0 |
| `BenchmarkRowsNext/binary=false/columns=32/value=blob` | 2,821.0 | 2,737.0 | -3.0% | 769 | 32 |
| `BenchmarkRowsNext/binary=true/columns=1/value=123456789` | 37.3 | 37.9 | +1.4% | 8 | 1 |
| `BenchmarkRowsNext/binary=true/columns=1/value=42` | 30.7 | 31.9 | +3.6% | 0 | 0 |
| `BenchmarkRowsNext/binary=true/columns=1/value=blob` | 106.6 | 102.6 | -3.8% | 24 | 1 |
| `BenchmarkRowsNext/binary=true/columns=32/value=123456789` | 423.2 | 417.5 | -1.3% | 256 | 32 |
| `BenchmarkRowsNext/binary=true/columns=32/value=42` | 225.5 | 227.1 | +0.7% | 0 | 0 |
| `BenchmarkRowsNext/binary=true/columns=32/value=blob` | 2,813.0 | 2,845.0 | +1.1% | 769 | 32 |
| `BenchmarkRowsScan/any/binary=false/rows=1/direct=false` | 76.0 | 77.6 | +2.1% | 80 | 4 |
| `BenchmarkRowsScan/any/binary=false/rows=1/direct=true` | 62.1 | 64.3 | +3.5% | 64 | 3 |
| `BenchmarkRowsScan/any/binary=false/rows=100/direct=false` | 6,442.0 | 6,658.0 | +3.4% | 6,416 | 301 |
| `BenchmarkRowsScan/any/binary=false/rows=100/direct=true` | 5,096.0 | 5,376.0 | +5.5% | 4,024 | 201 |
| `BenchmarkRowsScan/any/binary=true/rows=1/direct=false` | 80.6 | 81.8 | +1.6% | 80 | 4 |
| `BenchmarkRowsScan/any/binary=true/rows=1/direct=true` | 66.6 | 69.4 | +4.3% | 64 | 3 |
| `BenchmarkRowsScan/any/binary=true/rows=100/direct=false` | 6,830.0 | 7,157.0 | +4.8% | 6,416 | 301 |
| `BenchmarkRowsScan/any/binary=true/rows=100/direct=true` | 5,716.0 | 5,968.0 | +4.4% | 4,024 | 201 |
| `BenchmarkRowsScan/bytes/binary=false/rows=1/direct=false` | 66.1 | 68.0 | +2.8% | 56 | 3 |
| `BenchmarkRowsScan/bytes/binary=false/rows=1/direct=true` | 52.9 | 55.4 | +4.7% | 40 | 2 |
| `BenchmarkRowsScan/bytes/binary=false/rows=100/direct=false` | 5,512.0 | 5,748.0 | +4.3% | 4,016 | 201 |
| `BenchmarkRowsScan/bytes/binary=false/rows=100/direct=true` | 4,124.0 | 4,367.0 | +5.9% | 1,624 | 101 |
| `BenchmarkRowsScan/bytes/binary=true/rows=1/direct=false` | 70.1 | 73.3 | +4.4% | 56 | 3 |
| `BenchmarkRowsScan/bytes/binary=true/rows=1/direct=true` | 57.5 | 60.2 | +4.8% | 40 | 2 |
| `BenchmarkRowsScan/bytes/binary=true/rows=100/direct=false` | 5,891.0 | 6,195.0 | +5.2% | 4,016 | 201 |
| `BenchmarkRowsScan/bytes/binary=true/rows=100/direct=true` | 4,632.0 | 4,927.0 | +6.4% | 1,624 | 101 |
| `BenchmarkRowsScan/date-any/binary=false/rows=1/direct=false` | 77.6 | 77.9 | +0.4% | 80 | 4 |
| `BenchmarkRowsScan/date-any/binary=false/rows=1/direct=true` | 63.1 | 64.8 | +2.6% | 64 | 3 |
| `BenchmarkRowsScan/date-any/binary=false/rows=100/direct=false` | 6,549.0 | 6,798.0 | +3.8% | 6,416 | 301 |
| `BenchmarkRowsScan/date-any/binary=false/rows=100/direct=true` | 5,212.0 | 5,433.0 | +4.2% | 4,024 | 201 |
| `BenchmarkRowsScan/date-any/binary=true/rows=1/direct=false` | 96.8 | 98.5 | +1.8% | 96 | 5 |
| `BenchmarkRowsScan/date-any/binary=true/rows=1/direct=true` | 83.4 | 85.0 | +1.9% | 80 | 4 |
| `BenchmarkRowsScan/date-any/binary=true/rows=100/direct=false` | 8,544.0 | 8,766.0 | +2.6% | 8,016 | 401 |
| `BenchmarkRowsScan/date-any/binary=true/rows=100/direct=true` | 7,194.0 | 7,451.0 | +3.6% | 5,624 | 301 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=1/direct=false` | 66.6 | 68.4 | +2.6% | 56 | 3 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=1/direct=true` | 53.6 | 55.7 | +3.9% | 40 | 2 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=100/direct=false` | 5,593.0 | 5,807.0 | +3.8% | 4,016 | 201 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=100/direct=true` | 4,214.0 | 4,456.0 | +5.7% | 1,624 | 101 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=1/direct=false` | 86.6 | 89.6 | +3.5% | 72 | 4 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=1/direct=true` | 73.1 | 74.6 | +2.1% | 56 | 3 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=100/direct=false` | 7,587.0 | 7,787.0 | +2.6% | 5,616 | 301 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=100/direct=true` | 6,050.0 | 6,353.0 | +5.0% | 3,224 | 201 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=1/direct=false` | 53.8 | 55.8 | +3.7% | 40 | 2 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=1/direct=true` | 41.8 | 44.1 | +5.6% | 24 | 1 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=100/direct=false` | 4,357.0 | 4,604.0 | +5.7% | 2,416 | 101 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=100/direct=true` | 3,020.0 | 3,307.0 | +9.5% | 24 | 1 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=1/direct=false` | 74.2 | 76.6 | +3.2% | 56 | 3 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=1/direct=true` | 61.7 | 64.0 | +3.7% | 40 | 2 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=100/direct=false` | 6,344.0 | 6,572.0 | +3.6% | 4,016 | 201 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=100/direct=true` | 4,942.0 | 5,245.0 | +6.1% | 1,624 | 101 |
| `BenchmarkRowsScan/date-string/binary=false/rows=1/direct=false` | 64.4 | 65.8 | +2.2% | 56 | 3 |
| `BenchmarkRowsScan/date-string/binary=false/rows=1/direct=true` | 50.8 | 53.3 | +4.9% | 40 | 2 |
| `BenchmarkRowsScan/date-string/binary=false/rows=100/direct=false` | 5,341.0 | 5,564.0 | +4.2% | 4,016 | 201 |
| `BenchmarkRowsScan/date-string/binary=false/rows=100/direct=true` | 3,925.0 | 4,227.0 | +7.7% | 1,624 | 101 |
| `BenchmarkRowsScan/date-string/binary=true/rows=1/direct=false` | 83.5 | 85.8 | +2.7% | 72 | 4 |
| `BenchmarkRowsScan/date-string/binary=true/rows=1/direct=true` | 70.3 | 72.7 | +3.5% | 56 | 3 |
| `BenchmarkRowsScan/date-string/binary=true/rows=100/direct=false` | 7,252.0 | 7,539.0 | +4.0% | 5,616 | 301 |
| `BenchmarkRowsScan/date-string/binary=true/rows=100/direct=true` | 5,801.0 | 6,063.0 | +4.5% | 3,224 | 201 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=1/direct=false` | 87.8 | 89.0 | +1.4% | 40 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=1/direct=true` | 88.1 | 89.8 | +1.9% | 48 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=100/direct=false` | 7,861.0 | 7,970.0 | +1.4% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=100/direct=true` | 7,647.0 | 7,903.0 | +3.3% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=1/direct=false` | 77.3 | 78.8 | +1.9% | 40 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=1/direct=true` | 77.0 | 77.7 | +0.8% | 48 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=100/direct=false` | 6,707.0 | 6,833.0 | +1.9% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=100/direct=true` | 6,559.0 | 6,677.0 | +1.8% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=1/direct=false` | 62.9 | 64.6 | +2.7% | 64 | 3 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=1/direct=true` | 49.5 | 51.4 | +4.0% | 48 | 2 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=100/direct=false` | 5,179.0 | 5,418.0 | +4.6% | 4,816 | 201 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=100/direct=true` | 3,781.0 | 4,033.0 | +6.7% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=1/direct=false` | 85.3 | 87.9 | +3.0% | 88 | 4 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=1/direct=true` | 72.0 | 74.6 | +3.5% | 72 | 3 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=100/direct=false` | 7,451.0 | 7,721.0 | +3.6% | 7,216 | 301 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=100/direct=true` | 5,987.0 | 6,234.0 | +4.1% | 4,824 | 201 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=1/direct=false` | 82.0 | 83.0 | +1.1% | 40 | 2 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=1/direct=true` | 68.4 | 68.7 | +0.5% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=100/direct=false` | 7,122.0 | 7,284.0 | +2.3% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=100/direct=true` | 5,561.0 | 5,788.0 | +4.1% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=1/direct=false` | 70.4 | 72.0 | +2.3% | 40 | 2 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=1/direct=true` | 56.7 | 58.4 | +2.9% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=100/direct=false` | 6,021.0 | 6,165.0 | +2.4% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=100/direct=true` | 4,555.0 | 4,710.0 | +3.4% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=false/rows=1/direct=false` | 92.9 | 94.9 | +2.2% | 24 | 2 |
| `BenchmarkRowsScan/int64/binary=false/rows=1/direct=true` | 61.7 | 64.9 | +5.2% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=false/rows=100/direct=false` | 8,164.0 | 8,488.0 | +4.0% | 816 | 101 |
| `BenchmarkRowsScan/int64/binary=false/rows=100/direct=true` | 5,078.0 | 5,265.0 | +3.7% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=true/rows=1/direct=false` | 73.9 | 75.3 | +2.0% | 24 | 2 |
| `BenchmarkRowsScan/int64/binary=true/rows=1/direct=true` | 43.3 | 44.1 | +1.8% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=true/rows=100/direct=false` | 6,043.0 | 6,329.0 | +4.7% | 816 | 101 |
| `BenchmarkRowsScan/int64/binary=true/rows=100/direct=true` | 3,299.0 | 3,364.0 | +2.0% | 24 | 1 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=1/direct=false` | 100.0 | 102.7 | +2.7% | 24 | 2 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=1/direct=true` | 100.9 | 102.2 | +1.3% | 32 | 2 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=100/direct=false` | 8,946.0 | 9,212.0 | +3.0% | 816 | 101 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=100/direct=true` | 8,934.0 | 9,299.0 | +4.1% | 824 | 101 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=1/direct=false` | 81.5 | 82.5 | +1.3% | 24 | 2 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=1/direct=true` | 82.1 | 82.9 | +1.0% | 32 | 2 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=100/direct=false` | 6,967.0 | 7,019.0 | +0.7% | 816 | 101 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=100/direct=true` | 7,018.0 | 7,203.0 | +2.6% | 824 | 101 |
| `BenchmarkRowsScan/string/binary=false/rows=1/direct=false` | 63.9 | 65.3 | +2.3% | 56 | 3 |
| `BenchmarkRowsScan/string/binary=false/rows=1/direct=true` | 50.7 | 53.1 | +4.9% | 40 | 2 |
| `BenchmarkRowsScan/string/binary=false/rows=100/direct=false` | 5,287.0 | 5,524.0 | +4.5% | 4,016 | 201 |
| `BenchmarkRowsScan/string/binary=false/rows=100/direct=true` | 3,897.0 | 4,218.0 | +8.2% | 1,624 | 101 |
| `BenchmarkRowsScan/string/binary=true/rows=1/direct=false` | 67.6 | 70.0 | +3.4% | 56 | 3 |
| `BenchmarkRowsScan/string/binary=true/rows=1/direct=true` | 55.4 | 58.4 | +5.5% | 40 | 2 |
| `BenchmarkRowsScan/string/binary=true/rows=100/direct=false` | 5,649.0 | 5,985.0 | +5.9% | 4,016 | 201 |
| `BenchmarkRowsScan/string/binary=true/rows=100/direct=true` | 4,398.0 | 4,735.0 | +7.7% | 1,624 | 101 |
| `BenchmarkRowsScan/time-string/binary=false/rows=1/direct=false` | 62.3 | 66.9 | +7.4% | 48 | 3 |
| `BenchmarkRowsScan/time-string/binary=false/rows=1/direct=true` | 49.8 | 51.8 | +4.1% | 32 | 2 |
| `BenchmarkRowsScan/time-string/binary=false/rows=100/direct=false` | 5,261.0 | 5,414.0 | +2.9% | 3,216 | 201 |
| `BenchmarkRowsScan/time-string/binary=false/rows=100/direct=true` | 3,813.0 | 4,070.0 | +6.7% | 824 | 101 |
| `BenchmarkRowsScan/time-string/binary=true/rows=1/direct=false` | 85.1 | 88.0 | +3.5% | 64 | 4 |
| `BenchmarkRowsScan/time-string/binary=true/rows=1/direct=true` | 71.1 | 75.0 | +5.5% | 48 | 3 |
| `BenchmarkRowsScan/time-string/binary=true/rows=100/direct=false` | 7,524.0 | 7,742.0 | +2.9% | 4,816 | 301 |
| `BenchmarkRowsScan/time-string/binary=true/rows=100/direct=true` | 5,962.0 | 6,187.0 | +3.8% | 2,424 | 201 |

These are Go-only measurements, not a speed comparison with the Haskell driver or Connector/C. Metadata controls use identical SQL, scanner and network-accounting wrappers; only the advertised metadata-cache capability changes.

## Reproduce and preserve history

See [validation instructions](README.md), [review findings](../docs/MARIADB_REVIEW.md) and [implementation roadmap](../docs/ROADMAP.md).

Raw logs, hashes, process repetitions and server settings are in the capture JSON and compressed baseline logs. A nonzero full-run exit is retained when correctness fails; benchmark completion does not turn it into a pass.
