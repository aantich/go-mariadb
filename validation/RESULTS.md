# Go MariaDB live validation and benchmarks

> Dedicated MariaDB driver, Alpha. Baseline = unchanged upstream; Current = the measured working tree below.

Recorded: **2026-11-12T08:20:38.200863+00:00** · Parent commit: `e6232456bb6e` · Go: `go version go1.27.4 darwin/arm64`

Inputs: `7e0376b88d5fce25d372680fece6d1faa3c4af6cf10bf8775616b4be79ea5b97` · Matches committed source: **False**. See capture JSON for the complete dirty tree and per-file hashes.

Server: **12.3.3-MariaDB-log** · Host: `macOS-26.4-arm64-arm-64bit-Mach-O` · GOMAXPROCS: **4**.

Benchmark conditions: **No fresh idle confirmation. Correctness and isolated Linux validation complete before benchmark timing; per-family CPU observations retained. This capture includes all review corrections and prepare-write cancellation identity.**. Seven independent processes per benchmark family; medians and raw repetitions are retained.

## Current stage at a glance

🟢 **1029 passing assertions** · 🔴 **1 failed assertions** · ⏭ **7 skips**.
Ordinary totals exclude repeated race checks. The desired clean-session assertion remains an API gap; other failures are listed below.
The complete workflow exits **1**. Readiness remains **Alpha**; automated coverage does not establish production application readiness.

All **200 benchmark profiles** require **seven independent process repetitions**. Original passing assertions lost: **0**.
**Performance acceptance remains open:** 172 original profiles exceed +5% time, 76 exceed +20%, and 2 improve by more than 5%.
These are investigation thresholds, not proof of a code-caused change. Measurement conditions and per-family host observations remain in the capture.

| Rows per transaction | Prepared loop, current ms | Native bulk, current ms | Prepared/native throughput ratio | Text batch, current ms | LOCAL INFILE, current ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.253 | 0.315 | 0.80× | 0.262 | 0.310 |
| 16 | 0.866 | 0.338 | 2.56× | 0.309 | 0.323 |
| 100 | 4.789 | 0.734 | 6.53× | 0.522 | 0.573 |
| 1000 | 47.912 | 2.982 | 16.07× | 4.009 | 4.650 |

These compare matched API alternatives on the current driver. Text/TSV data and parameter matrices are prebuilt; native encoding remains timed. Every alternative includes the same transaction, deletion and count/sum/content verification. Native bulk includes transient prepare/close; the prepared control reuses its handle. These are not universal fork-versus-upstream speedups.

The [review corrections](../docs/STAGE1_REVIEW_FIXES.md), [implementation record](../docs/DEDICATED_DRIVER.md) and [coverage limits](COVERAGE.md) describe supported paths and remaining work. Original references and earlier captures remain unchanged.

## Correctness

| Suite | PASS | FAIL | SKIP | Exit |
| --- | ---: | ---: | ---: | ---: |
| upstream | 727 | 0 | 7 | 0 |
| protocol | 78 | 0 | 0 | 0 |
| mariadb | 224 | 1 | 0 | 1 |
| race | 951 | 1 | 7 | 1 |
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

Leaf cases are counted once. Race cases repeat correctness coverage; do not add them to the ordinary total. Vet/build rows have no individual test cases.

## Failed assertions

| Suite | Baseline failures | Current failures |
| --- | ---: | ---: |
| upstream | 0 | 0 |
| protocol | 76 | 0 |
| mariadb | 3 | 1 |

<details><summary>mariadb: 1 failed assertions</summary>

- `github.com/aantich/go-mariadb/validation/TestMariaDBDesiredCleanPoolReset`

</details>

<details><summary>race: 1 failed assertions</summary>

- `github.com/aantich/go-mariadb/validation/TestMariaDBDesiredCleanPoolReset`

</details>

## Capability inventory

| Capability | Baseline | Current | Evidence / next step |
| --- | --- | --- | --- |
| MariaDB metadata caching | SUPPORTED | SUPPORTED | packets.go:initCapabilities; validation metadata cached/uncached wire-byte control |
| ed25519 authentication | SUPPORTED | SUPPORTED | auth_ed25519.go; real ed25519 account |
| Unix/TLS/X.509 authentication | SUPPORTED | SUPPORTED | real socket identity and CA/hostname/client-certificate tests |
| large []byte parameters | SUPPORTED | SUPPORTED | existing COM_STMT_SEND_LONG_DATA; 2 MiB value/checksum checks |
| LOCAL INFILE reader | SUPPORTED | SUPPORTED | RegisterReaderHandler; real data/count/sum verification |
| Go 1.27 direct typed row scanning | SUPPORTED | SUPPORTED | rows_column_scanner.go; inherited RowsScan and typed/RawBytes benchmarks |
| native binary bulk execution | UNSUPPORTED_API | SUPPORTED | Opt-in BulkExec; real 1/16/100/1000-row tests, unsigned/NULL/DEFAULT/IGNORE, errors and validation-before-send. Aggregate results only. |
| per-unit bulk results | UNSUPPORTED_API | UNSUPPORTED_API | clientUnitBulkResult defined but not negotiated or exposed |
| extended type metadata | UNSUPPORTED_API | UNSUPPORTED_API | clientExtendedMetadata defined but not negotiated/parsed/exposed |
| session-state observation | UNSUPPORTED_API | UNSUPPORTED_API | clientSessionTrack not negotiated; OK state-change payload not exposed |
| clean connection reset/restoration | UNSUPPORTED_API | UNSUPPORTED_API | ResetSession performs stale-connection checks only; desired-isolation assertion fails |
| server cursor fetch | UNSUPPORTED_API | UNSUPPORTED_API | comStmtFetch constant exists; execute flags do not request cursors; no public fetch API |
| io.Reader prepared parameters | UNSUPPORTED_API | UNSUPPORTED_API | large []byte parameters supported, but converter has no streamed io.Reader parameter contract |
| progress callbacks | UNSUPPORTED_API | UNSUPPORTED_API | progressIndicator not negotiated; no callback/routing contract |
| PARSEC authentication | UNSUPPORTED_API | SUPPORTED | Built-in parsec plugin; real MariaDB account succeeds, binary challenge/signature and malformed/work-factor unit checks. |
| interactive PAM authentication | UNSUPPORTED_API | UNSUPPORTED_API | generic plugin extension exists; no built-in dialog/prompt policy or real PAM fixture |
| Kerberos GSSAPI authentication | UNSUPPORTED_API | UNSUPPORTED_API | generic plugin extension exists; no built-in GSSAPI/service-principal policy or real KDC fixture |
| transaction-aware CDC/GTID resume | UNSUPPORTED_API | UNSUPPORTED_API | no binlog consumer API; separate optional extension scope beyond database/sql |
| performance_schema instrumentation | ENVIRONMENT | ENVIRONMENT | local service has performance_schema=OFF; do not change shared server configuration |
| PAM/Kerberos services | ENVIRONMENT | ENVIRONMENT | isolated service fixtures not provisioned in this first Mac profile; do not claim live coverage |

Inventory entries are source-reviewed API gaps, not passing feature tests. Missing external services are recorded separately from missing APIs. See [coverage and limits](COVERAGE.md).

## Benchmark comparison

Baseline = immutable unchanged-upstream capture for original workloads; [first native-bulk capture](baselines/native-bulk-stage1-macos-arm64-mariadb-12.3.3-go1.27.4/capture.json) for the new matched bulk profiles. Current = latest complete capture. Change = `(Current / Baseline - 1) × 100`; negative means less time. Units are **ns/operation**. The original added live reads consume 1,000 rows, BLOB reads consume 1 MiB, and original added writes replace 100 rows transactionally. Native bulk alternatives use **1/16/100/1000 rows per transaction**. Inherited workloads retain their own row counts and decoder/scanner units; do not apply the live-read row count to every inherited benchmark. Connection operations establish and close one connection.

| Benchmark | Baseline ns/op | Current ns/op | Change vs baseline | Current B/op | Current allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| `BenchmarkExec` | 7,838.0 | 12,624.0 | +61.1% | 155 | 8 |
| `BenchmarkExecContext/1` | 10,338.0 | 22,803.0 | +120.6% | 152 | 8 |
| `BenchmarkExecContext/2` | 8,028.0 | 17,894.0 | +122.9% | 152 | 8 |
| `BenchmarkExecContext/3` | 7,531.0 | 15,294.0 | +103.1% | 152 | 8 |
| `BenchmarkExecContext/4` | 7,167.0 | 13,887.0 | +93.8% | 152 | 8 |
| `BenchmarkInterpolation` | 221.8 | 239.6 | +8.0% | 176 | 1 |
| `BenchmarkMariaDBConnect/socket` | 76,531.0 | 94,123.0 | +23.0% | 6,229 | 35 |
| `BenchmarkMariaDBConnect/text` | 141,123.0 | 157,299.0 | +11.5% | 6,486 | 43 |
| `BenchmarkMariaDBConnect/tls` | 1,174,989.0 | 1,263,674.0 | +7.5% | 86,008 | 542 |
| `BenchmarkMariaDBMetadata/cached` | 31,077.0 | 32,702.0 | +5.2% | 1,840 | 14 |
| `BenchmarkMariaDBMetadata/uncached` | 33,527.0 | 34,031.0 | +1.5% | 3,632 | 15 |
| `BenchmarkMariaDBNativeBulk/1/local_infile` | 267,344.0 | 309,945.0 | +15.9% | 21,192 | 57 |
| `BenchmarkMariaDBNativeBulk/1/native_bulk` | 262,800.0 | 315,147.0 | +19.9% | 2,360 | 50 |
| `BenchmarkMariaDBNativeBulk/1/prepared` | 231,833.0 | 253,191.0 | +9.2% | 2,376 | 50 |
| `BenchmarkMariaDBNativeBulk/1/text_batch` | 245,576.0 | 261,702.0 | +6.6% | 2,680 | 54 |
| `BenchmarkMariaDBNativeBulk/100/local_infile` | 460,474.0 | 573,199.0 | +24.5% | 21,200 | 58 |
| `BenchmarkMariaDBNativeBulk/100/native_bulk` | 455,482.0 | 733,658.0 | +61.1% | 8,673 | 53 |
| `BenchmarkMariaDBNativeBulk/100/prepared` | 3,622,989.0 | 4,788,602.0 | +32.2% | 19,815 | 546 |
| `BenchmarkMariaDBNativeBulk/100/text_batch` | 458,859.0 | 521,671.0 | +13.7% | 2,688 | 55 |
| `BenchmarkMariaDBNativeBulk/1000/local_infile` | 2,798,302.0 | 4,649,542.0 | +66.2% | 21,206 | 58 |
| `BenchmarkMariaDBNativeBulk/1000/native_bulk` | 2,870,575.0 | 2,981,684.0 | +3.9% | 61,716 | 53 |
| `BenchmarkMariaDBNativeBulk/1000/prepared` | 34,907,736.0 | 47,912,183.0 | +37.3% | 178,281 | 5,046 |
| `BenchmarkMariaDBNativeBulk/1000/text_batch` | 2,668,387.0 | 4,009,372.0 | +50.3% | 2,692 | 55 |
| `BenchmarkMariaDBNativeBulk/16/local_infile` | 307,305.0 | 323,091.0 | +5.1% | 21,200 | 58 |
| `BenchmarkMariaDBNativeBulk/16/native_bulk` | 297,750.0 | 337,685.0 | +13.4% | 3,216 | 52 |
| `BenchmarkMariaDBNativeBulk/16/prepared` | 789,377.0 | 865,703.0 | +9.7% | 5,025 | 126 |
| `BenchmarkMariaDBNativeBulk/16/text_batch` | 275,142.0 | 308,964.0 | +12.3% | 2,688 | 55 |
| `BenchmarkMariaDBPool/connections_1` | 31,564.0 | 37,541.0 | +18.9% | 664 | 20 |
| `BenchmarkMariaDBPool/connections_8` | 10,870.0 | 15,462.0 | +42.2% | 521 | 17 |
| `BenchmarkMariaDBRead/compressed/blob/raw=false` | 5,107,048.0 | 5,284,220.0 | +3.5% | 1,062,034 | 26 |
| `BenchmarkMariaDBRead/compressed/blob/raw=true` | 5,296,447.0 | 5,589,852.0 | +5.5% | 648 | 22 |
| `BenchmarkMariaDBRead/compressed/narrow/raw=false` | 1,200,599.0 | 1,190,102.0 | -0.9% | 26,926 | 2,966 |
| `BenchmarkMariaDBRead/compressed/narrow/raw=true` | 1,269,046.0 | 1,154,562.0 | -9.0% | 15,512 | 1,712 |
| `BenchmarkMariaDBRead/compressed/wide/raw=false` | 1,882,802.0 | 1,912,121.0 | +1.6% | 3,904 | 68 |
| `BenchmarkMariaDBRead/compressed/wide/raw=true` | 1,905,227.0 | 2,090,553.0 | +9.7% | 196,905 | 23,913 |
| `BenchmarkMariaDBRead/prepared/blob/raw=false` | 2,434,221.0 | 2,597,854.0 | +6.7% | 2,105,855 | 19 |
| `BenchmarkMariaDBRead/prepared/blob/raw=true` | 2,314,439.0 | 3,187,893.0 | +37.7% | 1,057,264 | 17 |
| `BenchmarkMariaDBRead/prepared/narrow/raw=false` | 226,600.0 | 249,485.0 | +10.1% | 24,048 | 2,917 |
| `BenchmarkMariaDBRead/prepared/narrow/raw=true` | 241,120.0 | 254,540.0 | +5.6% | 12,992 | 1,663 |
| `BenchmarkMariaDBRead/prepared/wide/raw=false` | 496,080.0 | 652,820.0 | +31.6% | 2,608 | 17 |
| `BenchmarkMariaDBRead/prepared/wide/raw=true` | 1,317,961.0 | 1,548,903.0 | +17.5% | 194,216 | 23,862 |
| `BenchmarkMariaDBRead/socket/blob/raw=false` | 3,304,196.0 | 3,676,297.0 | +11.3% | 2,105,884 | 19 |
| `BenchmarkMariaDBRead/socket/blob/raw=true` | 3,251,866.0 | 3,411,057.0 | +4.9% | 1,057,297 | 17 |
| `BenchmarkMariaDBRead/socket/narrow/raw=false` | 282,040.0 | 298,368.0 | +5.8% | 36,664 | 4,564 |
| `BenchmarkMariaDBRead/socket/narrow/raw=true` | 247,567.0 | 260,459.0 | +5.2% | 13,072 | 1,664 |
| `BenchmarkMariaDBRead/socket/wide/raw=false` | 2,349,018.0 | 2,516,960.0 | +7.1% | 401,865 | 52,720 |
| `BenchmarkMariaDBRead/socket/wide/raw=true` | 1,472,006.0 | 1,768,665.0 | +20.2% | 196,064 | 23,893 |
| `BenchmarkMariaDBRead/text/blob/raw=false` | 2,389,052.0 | 2,425,011.0 | +1.5% | 2,105,891 | 19 |
| `BenchmarkMariaDBRead/text/blob/raw=true` | 2,269,412.0 | 2,384,571.0 | +5.1% | 1,057,296 | 17 |
| `BenchmarkMariaDBRead/text/narrow/raw=false` | 512,291.0 | 578,791.0 | +13.0% | 36,664 | 4,564 |
| `BenchmarkMariaDBRead/text/narrow/raw=true` | 536,490.0 | 504,960.0 | -5.9% | 13,072 | 1,664 |
| `BenchmarkMariaDBRead/text/wide/raw=false` | 2,715,388.0 | 2,948,749.0 | +8.6% | 401,865 | 52,720 |
| `BenchmarkMariaDBRead/text/wide/raw=true` | 2,035,901.0 | 2,299,098.0 | +12.9% | 196,064 | 23,893 |
| `BenchmarkMariaDBRead/tls/blob/raw=false` | 2,695,418.0 | 2,955,741.0 | +9.7% | 2,106,325 | 22 |
| `BenchmarkMariaDBRead/tls/blob/raw=true` | 2,707,999.0 | 3,073,840.0 | +13.5% | 1,057,498 | 18 |
| `BenchmarkMariaDBRead/tls/narrow/raw=false` | 341,167.0 | 363,250.0 | +6.5% | 36,670 | 4,564 |
| `BenchmarkMariaDBRead/tls/narrow/raw=true` | 267,864.0 | 288,444.0 | +7.7% | 13,073 | 1,664 |
| `BenchmarkMariaDBRead/tls/wide/raw=false` | 2,402,114.0 | 2,795,892.0 | +16.4% | 401,984 | 52,720 |
| `BenchmarkMariaDBRead/tls/wide/raw=true` | 1,693,686.0 | 2,166,787.0 | +27.9% | 196,499 | 23,893 |
| `BenchmarkMariaDBWrite/load_data_reader_100` | 451,136.0 | 478,223.0 | +6.0% | 19,828 | 47 |
| `BenchmarkMariaDBWrite/sequential_prepared_100` | 3,606,958.0 | 3,827,269.0 | +6.1% | 23,092 | 843 |
| `BenchmarkMariaDBWrite/text_batch_100` | 444,333.0 | 463,244.0 | +4.3% | 1,356 | 44 |
| `BenchmarkParseDSN` | 6,146.0 | 7,881.0 | +28.2% | 16,618 | 130 |
| `BenchmarkQuery` | 10,459.0 | 22,035.0 | +110.7% | 575 | 19 |
| `BenchmarkQueryCompressed` | 10,496.0 | 24,706.0 | +135.4% | 584 | 19 |
| `BenchmarkQueryContext/1` | 11,385.0 | 24,930.0 | +119.0% | 552 | 18 |
| `BenchmarkQueryContext/2` | 8,507.0 | 22,531.0 | +164.9% | 552 | 18 |
| `BenchmarkQueryContext/3` | 7,763.0 | 15,391.0 | +98.3% | 552 | 18 |
| `BenchmarkQueryContext/4` | 7,415.0 | 15,072.0 | +103.3% | 552 | 18 |
| `BenchmarkQueryRawBytes/size=100` | 1,116,598.0 | 1,889,811.0 | +69.2% | 3,180 | 119 |
| `BenchmarkQueryRawBytes/size=1000` | 1,063,091.0 | 2,923,475.0 | +175.0% | 3,210 | 122 |
| `BenchmarkQueryRawBytes/size=12000` | 1,452,578.0 | 2,989,758.0 | +105.8% | 3,395 | 122 |
| `BenchmarkQueryRawBytes/size=16000` | 1,511,778.0 | 2,885,275.0 | +90.9% | 3,396 | 122 |
| `BenchmarkQueryRawBytes/size=2000` | 1,151,954.0 | 1,795,541.0 | +55.9% | 3,210 | 122 |
| `BenchmarkQueryRawBytes/size=256000` | 4,152,874.0 | 7,019,263.0 | +69.0% | 10,236 | 123 |
| `BenchmarkQueryRawBytes/size=32000` | 1,870,201.0 | 2,906,696.0 | +55.4% | 3,630 | 122 |
| `BenchmarkQueryRawBytes/size=4000` | 1,191,269.0 | 2,156,294.0 | +81.0% | 3,212 | 122 |
| `BenchmarkQueryRawBytes/size=64000` | 2,192,162.0 | 3,573,558.0 | +63.0% | 4,406 | 122 |
| `BenchmarkQueryRawBytes/size=8000` | 1,374,434.0 | 2,152,890.0 | +56.6% | 3,309 | 122 |
| `BenchmarkReceive10kRows/query` | 2,172,409.0 | 3,434,983.0 | +58.1% | 608 | 18 |
| `BenchmarkReceive10kRowsCompressed/query` | 6,099,741.0 | 7,256,827.0 | +19.0% | 75,361 | 2,247 |
| `BenchmarkReceiveMetadata/query` | 864,221.0 | 1,254,018.0 | +45.1% | 41,415 | 14 |
| `BenchmarkRoundtripBin` | 37,003.0 | 44,153.0 | +19.3% | 581 | 17 |
| `BenchmarkRoundtripTxt` | 47,253.0 | 54,373.0 | +15.1% | 5,521 | 16 |
| `BenchmarkRowsNext/binary=false/columns=1/value=123456789` | 56.9 | 71.1 | +25.0% | 8 | 1 |
| `BenchmarkRowsNext/binary=false/columns=1/value=42` | 42.7 | 54.4 | +27.5% | 0 | 0 |
| `BenchmarkRowsNext/binary=false/columns=1/value=blob` | 101.7 | 119.6 | +17.6% | 24 | 1 |
| `BenchmarkRowsNext/binary=false/columns=32/value=123456789` | 1,069.0 | 1,241.0 | +16.1% | 256 | 32 |
| `BenchmarkRowsNext/binary=false/columns=32/value=42` | 611.8 | 708.1 | +15.7% | 0 | 0 |
| `BenchmarkRowsNext/binary=false/columns=32/value=blob` | 2,821.0 | 3,848.0 | +36.4% | 769 | 32 |
| `BenchmarkRowsNext/binary=true/columns=1/value=123456789` | 37.3 | 40.4 | +8.2% | 8 | 1 |
| `BenchmarkRowsNext/binary=true/columns=1/value=42` | 30.7 | 35.9 | +16.6% | 0 | 0 |
| `BenchmarkRowsNext/binary=true/columns=1/value=blob` | 106.6 | 115.5 | +8.3% | 24 | 1 |
| `BenchmarkRowsNext/binary=true/columns=32/value=123456789` | 423.2 | 455.2 | +7.6% | 256 | 32 |
| `BenchmarkRowsNext/binary=true/columns=32/value=42` | 225.5 | 231.6 | +2.7% | 0 | 0 |
| `BenchmarkRowsNext/binary=true/columns=32/value=blob` | 2,813.0 | 3,295.0 | +17.1% | 769 | 32 |
| `BenchmarkRowsScan/any/binary=false/rows=1/direct=false` | 76.0 | 81.6 | +7.5% | 80 | 4 |
| `BenchmarkRowsScan/any/binary=false/rows=1/direct=true` | 62.1 | 66.4 | +6.9% | 64 | 3 |
| `BenchmarkRowsScan/any/binary=false/rows=100/direct=false` | 6,442.0 | 7,026.0 | +9.1% | 6,416 | 301 |
| `BenchmarkRowsScan/any/binary=false/rows=100/direct=true` | 5,096.0 | 5,510.0 | +8.1% | 4,024 | 201 |
| `BenchmarkRowsScan/any/binary=true/rows=1/direct=false` | 80.6 | 89.3 | +10.9% | 80 | 4 |
| `BenchmarkRowsScan/any/binary=true/rows=1/direct=true` | 66.6 | 75.7 | +13.7% | 64 | 3 |
| `BenchmarkRowsScan/any/binary=true/rows=100/direct=false` | 6,830.0 | 7,477.0 | +9.5% | 6,416 | 301 |
| `BenchmarkRowsScan/any/binary=true/rows=100/direct=true` | 5,716.0 | 6,199.0 | +8.4% | 4,024 | 201 |
| `BenchmarkRowsScan/bytes/binary=false/rows=1/direct=false` | 66.1 | 75.0 | +13.4% | 56 | 3 |
| `BenchmarkRowsScan/bytes/binary=false/rows=1/direct=true` | 52.9 | 74.3 | +40.4% | 40 | 2 |
| `BenchmarkRowsScan/bytes/binary=false/rows=100/direct=false` | 5,512.0 | 6,031.0 | +9.4% | 4,016 | 201 |
| `BenchmarkRowsScan/bytes/binary=false/rows=100/direct=true` | 4,124.0 | 4,476.0 | +8.5% | 1,624 | 101 |
| `BenchmarkRowsScan/bytes/binary=true/rows=1/direct=false` | 70.1 | 76.3 | +8.8% | 56 | 3 |
| `BenchmarkRowsScan/bytes/binary=true/rows=1/direct=true` | 57.5 | 70.5 | +22.6% | 40 | 2 |
| `BenchmarkRowsScan/bytes/binary=true/rows=100/direct=false` | 5,891.0 | 6,441.0 | +9.3% | 4,016 | 201 |
| `BenchmarkRowsScan/bytes/binary=true/rows=100/direct=true` | 4,632.0 | 5,565.0 | +20.1% | 1,624 | 101 |
| `BenchmarkRowsScan/date-any/binary=false/rows=1/direct=false` | 77.6 | 96.0 | +23.7% | 80 | 4 |
| `BenchmarkRowsScan/date-any/binary=false/rows=1/direct=true` | 63.1 | 76.3 | +20.8% | 64 | 3 |
| `BenchmarkRowsScan/date-any/binary=false/rows=100/direct=false` | 6,549.0 | 8,788.0 | +34.2% | 6,416 | 301 |
| `BenchmarkRowsScan/date-any/binary=false/rows=100/direct=true` | 5,212.0 | 7,712.0 | +48.0% | 4,024 | 201 |
| `BenchmarkRowsScan/date-any/binary=true/rows=1/direct=false` | 96.8 | 119.7 | +23.7% | 96 | 5 |
| `BenchmarkRowsScan/date-any/binary=true/rows=1/direct=true` | 83.4 | 101.7 | +22.0% | 80 | 4 |
| `BenchmarkRowsScan/date-any/binary=true/rows=100/direct=false` | 8,544.0 | 9,240.0 | +8.1% | 8,016 | 401 |
| `BenchmarkRowsScan/date-any/binary=true/rows=100/direct=true` | 7,194.0 | 7,893.0 | +9.7% | 5,624 | 301 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=1/direct=false` | 66.6 | 82.0 | +23.0% | 56 | 3 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=1/direct=true` | 53.6 | 70.4 | +31.3% | 40 | 2 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=100/direct=false` | 5,593.0 | 6,500.0 | +16.2% | 4,016 | 201 |
| `BenchmarkRowsScan/date-bytes/binary=false/rows=100/direct=true` | 4,214.0 | 5,542.0 | +31.5% | 1,624 | 101 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=1/direct=false` | 86.6 | 97.7 | +12.8% | 72 | 4 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=1/direct=true` | 73.1 | 82.6 | +13.1% | 56 | 3 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=100/direct=false` | 7,587.0 | 9,110.0 | +20.1% | 5,616 | 301 |
| `BenchmarkRowsScan/date-bytes/binary=true/rows=100/direct=true` | 6,050.0 | 7,594.0 | +25.5% | 3,224 | 201 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=1/direct=false` | 53.8 | 59.0 | +9.8% | 40 | 2 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=1/direct=true` | 41.8 | 59.8 | +43.2% | 24 | 1 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=100/direct=false` | 4,357.0 | 4,840.0 | +11.1% | 2,416 | 101 |
| `BenchmarkRowsScan/date-raw/binary=false/rows=100/direct=true` | 3,020.0 | 3,391.0 | +12.3% | 24 | 1 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=1/direct=false` | 74.2 | 98.7 | +33.0% | 56 | 3 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=1/direct=true` | 61.7 | 68.1 | +10.4% | 40 | 2 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=100/direct=false` | 6,344.0 | 6,963.0 | +9.8% | 4,016 | 201 |
| `BenchmarkRowsScan/date-raw/binary=true/rows=100/direct=true` | 4,942.0 | 6,117.0 | +23.8% | 1,624 | 101 |
| `BenchmarkRowsScan/date-string/binary=false/rows=1/direct=false` | 64.4 | 69.3 | +7.5% | 56 | 3 |
| `BenchmarkRowsScan/date-string/binary=false/rows=1/direct=true` | 50.8 | 54.7 | +7.7% | 40 | 2 |
| `BenchmarkRowsScan/date-string/binary=false/rows=100/direct=false` | 5,341.0 | 5,850.0 | +9.5% | 4,016 | 201 |
| `BenchmarkRowsScan/date-string/binary=false/rows=100/direct=true` | 3,925.0 | 4,594.0 | +17.0% | 1,624 | 101 |
| `BenchmarkRowsScan/date-string/binary=true/rows=1/direct=false` | 83.5 | 90.0 | +7.8% | 72 | 4 |
| `BenchmarkRowsScan/date-string/binary=true/rows=1/direct=true` | 70.3 | 74.0 | +5.4% | 56 | 3 |
| `BenchmarkRowsScan/date-string/binary=true/rows=100/direct=false` | 7,252.0 | 7,851.0 | +8.3% | 5,616 | 301 |
| `BenchmarkRowsScan/date-string/binary=true/rows=100/direct=true` | 5,801.0 | 6,267.0 | +8.0% | 3,224 | 201 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=1/direct=false` | 87.8 | 108.7 | +23.8% | 40 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=1/direct=true` | 88.1 | 98.9 | +12.3% | 48 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=100/direct=false` | 7,861.0 | 10,135.0 | +28.9% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=false/rows=100/direct=true` | 7,647.0 | 11,441.0 | +49.6% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=1/direct=false` | 77.3 | 87.8 | +13.5% | 40 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=1/direct=true` | 77.0 | 83.8 | +8.8% | 48 | 2 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=100/direct=false` | 6,707.0 | 8,349.0 | +24.5% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-nulltime/binary=true/rows=100/direct=true` | 6,559.0 | 8,017.0 | +22.2% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=1/direct=false` | 62.9 | 84.4 | +34.3% | 64 | 3 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=1/direct=true` | 49.5 | 62.6 | +26.6% | 48 | 2 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=100/direct=false` | 5,179.0 | 8,997.0 | +73.7% | 4,816 | 201 |
| `BenchmarkRowsScan/datetime-string/binary=false/rows=100/direct=true` | 3,781.0 | 4,531.0 | +19.8% | 2,424 | 101 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=1/direct=false` | 85.3 | 107.9 | +26.4% | 88 | 4 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=1/direct=true` | 72.0 | 101.6 | +41.0% | 72 | 3 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=100/direct=false` | 7,451.0 | 10,672.0 | +43.2% | 7,216 | 301 |
| `BenchmarkRowsScan/datetime-string/binary=true/rows=100/direct=true` | 5,987.0 | 7,823.0 | +30.7% | 4,824 | 201 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=1/direct=false` | 82.0 | 116.4 | +41.9% | 40 | 2 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=1/direct=true` | 68.4 | 98.1 | +43.4% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=100/direct=false` | 7,122.0 | 8,483.0 | +19.1% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-time/binary=false/rows=100/direct=true` | 5,561.0 | 8,340.0 | +50.0% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=1/direct=false` | 70.4 | 96.2 | +36.6% | 40 | 2 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=1/direct=true` | 56.7 | 69.0 | +21.6% | 24 | 1 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=100/direct=false` | 6,021.0 | 7,252.0 | +20.4% | 2,416 | 101 |
| `BenchmarkRowsScan/datetime-time/binary=true/rows=100/direct=true` | 4,555.0 | 5,264.0 | +15.6% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=false/rows=1/direct=false` | 92.9 | 104.8 | +12.8% | 24 | 2 |
| `BenchmarkRowsScan/int64/binary=false/rows=1/direct=true` | 61.7 | 69.8 | +13.0% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=false/rows=100/direct=false` | 8,164.0 | 8,938.0 | +9.5% | 816 | 101 |
| `BenchmarkRowsScan/int64/binary=false/rows=100/direct=true` | 5,078.0 | 5,426.0 | +6.9% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=true/rows=1/direct=false` | 73.9 | 77.8 | +5.3% | 24 | 2 |
| `BenchmarkRowsScan/int64/binary=true/rows=1/direct=true` | 43.3 | 55.3 | +27.8% | 24 | 1 |
| `BenchmarkRowsScan/int64/binary=true/rows=100/direct=false` | 6,043.0 | 6,456.0 | +6.8% | 816 | 101 |
| `BenchmarkRowsScan/int64/binary=true/rows=100/direct=true` | 3,299.0 | 3,571.0 | +8.2% | 24 | 1 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=1/direct=false` | 100.0 | 106.7 | +6.7% | 24 | 2 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=1/direct=true` | 100.9 | 114.5 | +13.5% | 32 | 2 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=100/direct=false` | 8,946.0 | 9,665.0 | +8.0% | 816 | 101 |
| `BenchmarkRowsScan/null-int64/binary=false/rows=100/direct=true` | 8,934.0 | 10,392.0 | +16.3% | 824 | 101 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=1/direct=false` | 81.5 | 86.0 | +5.6% | 24 | 2 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=1/direct=true` | 82.1 | 95.7 | +16.6% | 32 | 2 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=100/direct=false` | 6,967.0 | 7,186.0 | +3.1% | 816 | 101 |
| `BenchmarkRowsScan/null-int64/binary=true/rows=100/direct=true` | 7,018.0 | 7,188.0 | +2.4% | 824 | 101 |
| `BenchmarkRowsScan/string/binary=false/rows=1/direct=false` | 63.9 | 69.4 | +8.6% | 56 | 3 |
| `BenchmarkRowsScan/string/binary=false/rows=1/direct=true` | 50.7 | 56.3 | +11.0% | 40 | 2 |
| `BenchmarkRowsScan/string/binary=false/rows=100/direct=false` | 5,287.0 | 6,782.0 | +28.3% | 4,016 | 201 |
| `BenchmarkRowsScan/string/binary=false/rows=100/direct=true` | 3,897.0 | 4,259.0 | +9.3% | 1,624 | 101 |
| `BenchmarkRowsScan/string/binary=true/rows=1/direct=false` | 67.6 | 102.2 | +51.1% | 56 | 3 |
| `BenchmarkRowsScan/string/binary=true/rows=1/direct=true` | 55.4 | 63.3 | +14.3% | 40 | 2 |
| `BenchmarkRowsScan/string/binary=true/rows=100/direct=false` | 5,649.0 | 6,727.0 | +19.1% | 4,016 | 201 |
| `BenchmarkRowsScan/string/binary=true/rows=100/direct=true` | 4,398.0 | 5,000.0 | +13.7% | 1,624 | 101 |
| `BenchmarkRowsScan/time-string/binary=false/rows=1/direct=false` | 62.3 | 79.1 | +27.0% | 48 | 3 |
| `BenchmarkRowsScan/time-string/binary=false/rows=1/direct=true` | 49.8 | 55.9 | +12.4% | 32 | 2 |
| `BenchmarkRowsScan/time-string/binary=false/rows=100/direct=false` | 5,261.0 | 6,805.0 | +29.3% | 3,216 | 201 |
| `BenchmarkRowsScan/time-string/binary=false/rows=100/direct=true` | 3,813.0 | 4,814.0 | +26.3% | 824 | 101 |
| `BenchmarkRowsScan/time-string/binary=true/rows=1/direct=false` | 85.1 | 100.4 | +18.0% | 64 | 4 |
| `BenchmarkRowsScan/time-string/binary=true/rows=1/direct=true` | 71.1 | 91.1 | +28.2% | 48 | 3 |
| `BenchmarkRowsScan/time-string/binary=true/rows=100/direct=false` | 7,524.0 | 10,032.0 | +33.3% | 4,816 | 301 |
| `BenchmarkRowsScan/time-string/binary=true/rows=100/direct=true` | 5,962.0 | 6,704.0 | +12.4% | 2,424 | 201 |

These are Go-only measurements, not a speed comparison with the Haskell driver or Connector/C. Metadata controls use identical SQL, scanner and network-accounting wrappers; only the advertised metadata-cache capability changes.

## Reproduce and preserve history

See [validation instructions](README.md), [review findings](../docs/MARIADB_REVIEW.md) and [implementation roadmap](../docs/ROADMAP.md).

Raw logs, hashes, process repetitions and server settings are in the capture JSON and compressed baseline logs. A nonzero full-run exit is retained when correctness fails; benchmark completion does not turn it into a pass.
