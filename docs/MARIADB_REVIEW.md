# MariaDB Go driver: initial review

This is the historical unchanged-driver assessment. Anton subsequently chose a
[dedicated driver implementation](DEDICATED_DRIVER.md); current changes and
measurements are linked there. Source-unchanged statements below describe the
original review/baseline, not the current implementation working tree.

11 November 2026. This review and the [baseline](../validation/RESULTS.md) precede
driver implementation changes. Test failures are retained rather than fixed
before measuring the inherited implementation.

Follow-up before fixes: [upstream contribution and package assessment](UPSTREAM_STRATEGY.md)
maps overlapping open PRs and the existing capability/plugin architecture. It also
records a source-confirmed trailing-zero auth-challenge edge case not exercised
by the original captured workloads. That discovery does not change baseline
outcomes. The proposed distribution strategy is awaiting Anton's decision.

## Exact sources

* Go fork and public upstream master both resolve to
  `e6232456bb6eb92a117563823ac6956937410b96` (10 November 2026).
* The checked-out MariaDB server remains at
  `b439f350ce3a8306c77e64bc1679681320244d17` (2 November). The working tree was
  preserved. Current public main was fetched for read-only inspection:
  `ef4c7459b8433603b306dc46618eccae9d2b6957` (11 November).
* Runtime measurements use the installed MariaDB **12.3.3** service, not a build
  of server main. These are distinct source-review and runtime evidence profiles.
* The Go module retains its inherited `github.com/go-sql-driver/mysql` identity
  for this baseline. Imports resolve to the local checkout. The capture hashes
  every production Go file and verifies it still matches the original commit.

The initial unit-only run used an unavailable port explicitly and retained its
live-test skips. It is development evidence, not the live baseline. The full
baseline preflights the actual server and runs inherited live tests separately.

## What is already good

This is a considerably newer starting point than the original Haskell fork.
Do not mechanically port the Haskell changes or attribute inherited work to us.

* MariaDB's high capability word is already read/written; metadata caching is
  negotiated, prepared metadata is cached, and ordinary DDL reprepare works in
  the new live test.
* ed25519 works against a real account. Native password, socket identity,
  verified TLS, client certificates and the server's SHA-2 account also work.
* Context cancellation retires the protocol connection. Bounded pool
  concurrency, early row closure/draining and subsequent queries pass.
* Go 1.27 has a direct column-scanning implementation. Its typed integer path
  avoids legacy driver.Value boxing; there are already detailed scanner/decoder
  microbenchmarks. Preserve and measure both paths across Go versions.
* zlib compression, LOCAL INFILE reader handlers, large buffered parameters,
  server error code/SQLSTATE, unsigned value binding, and utf8mb4 are present.
* The buffer already caps its reusable backing storage at 256 KiB. Do not claim
  an unbounded reusable-buffer cache merely because large messages allocate.

All inherited correctness failures/skips and added test results are separately
visible in the capture. `go vet` and race detection are independent stages; a
nonzero race-suite exit from a known assertion is not itself a data-race report.

## Confirmed findings

| Priority | Finding | Reproduction / consequence | Direction |
| --- | --- | --- | --- |
| P1 | Truncated greetings panic | `TestMariaDBMalformedGreeting`: proper prefixes of a greeting reach unchecked slices in `readHandshakePacket`. A malformed server/proxy can terminate an application process. | Validate structure before indexing and return a stable protocol error; close an unusable connection. |
| P1 | Truncated OK/result/column metadata packets panic | `TestMariaDBMalformedOK`, `TestMariaDBMalformedMetadataHeader`, `TestMariaDBMalformedColumn`. Length-encoded values and fixed metadata tails lack sufficient bounds checks. Some incomplete column tails are accepted. | Checked parsers, overflow-safe size arithmetic and explicit metadata limits. Preserve the fast valid-packet path. |
| P1 | Metadata omission accepts an empty cache | `TestMariaDBMissingCachedMetadata` sends one declared result column with omitted metadata and no cached schema. The header is accepted with no matching columns. | Validate statement/result schema identity, count and lifetime before decoding rows. |
| P1 | Unsigned insert ID silently becomes negative | Real AUTO_INCREMENT value `9223372036854775808`; `LastInsertId()` succeeds with `-9223372036854775808`. `handleOkPacket` casts uint64 into int64; result accessors always return nil errors. | Preserve unsigned values internally; standard int64 accessors report overflow, with an explicit unsigned extension for callers. |
| P2 | No general receive-message budget | `TestMariaDBReceiveLimit`: 65 incoming bytes are accepted when the connection's `maxAllowedPacket` is 64. That setting is enforced on writes, not general reads. | Add a separately documented inbound logical-message budget, auth and metadata budgets; bound decompressed output too. This is a hardening gap, not a claim that the existing option promises a read limit. |
| P2 | PARSEC cannot authenticate | The installed real `parsec` account fails with unknown authentication plugin `parsec`. | Add a tested plugin using the existing extension mechanism. |
| P2 / API gap | Pool checkout preserves session state | `TestMariaDBDesiredCleanPoolReset`: a user variable survives returning and borrowing the sole pooled connection. | Offer explicit clean-session reset/restoration. Existing `database/sql` behavior does not promise arbitrary session sanitization; do not silently reinterpret this desired test as an existing contract. |

Numerous failed truncation prefixes represent a few parser defect classes, not
dozens of independent vulnerabilities. Authentication/metadata failures do not
establish that trusted, correctly configured servers emit these packets normally.
The severity concerns robustness against malformed peers and proxies.

## MariaDB API inventory and server comparison

The [inventory](../validation/capabilities.json) keeps unsupported APIs distinct
from environment gaps. Constants alone are not implementations.

* **Native bulk and per-unit results:** server
  [`include/mysql_com.h`](https://github.com/MariaDB/server/blob/ef4c7459b8433603b306dc46618eccae9d2b6957/include/mysql_com.h#L279)
  advertises bits 34/37; `sql/sql_prepare.cc` implements bulk execution. The Go
  driver defines the flags but negotiates only metadata caching and exposes no
  `COM_STMT_BULK_EXECUTE` path. This is the largest clear round-trip reduction
  opportunity for ingestion and batch updates.
* **Extended metadata:** server `sql/protocol.cc` emits additional type/format
  metadata when bit 35 is negotiated. The driver neither negotiates nor exposes
  it. Ordinary UUID/INET/JSON/vector wire/value expressions passing does not prove
  that the native type identity or an idiomatic domain type is available.
* **Session state and reset:** server `sql/sql_parse.cc` handles
  `COM_RESET_CONNECTION`, including session cleanup and authentication charset
  restoration. Go's `connection.go:ResetSession` only checks liveness. Session
  tracker payloads are not negotiated/exposed. Reset must invalidate statement
  generations and metadata, with explicit restoration policy.
* **Server cursors/progress:** `comStmtFetch` and progress constants exist, but
  execution always writes `CURSOR_TYPE_NO_CURSOR` and no public fetch/progress
  contract exists. These are resource/control features; benchmark any speed
  claim separately from their memory or observability benefits.
* **Streaming prepared parameters:** buffered long data exists. The current
  `writeCommandLongData` allocates a full parameter-sized staging buffer and
  copies the input. `io.Reader` sources need their own bounded, one-shot API,
  partial-upload/reset rules and retained-memory negative controls.
* **External authentication:** the generic plugin extension is useful, but
  built-in PARSEC, interactive PAM and GSSAPI policies are absent. PAM/Kerberos
  service fixtures are not provisioned in this initial profile. A generic plugin
  hook must not be described as verified support for those services.
* **CDC:** absent in this `database/sql` driver. Treat transaction-aware MariaDB
  GTID/binlog support as a separate optional package/project decision. It is not
  a prerequisite for excellent SQL-driver performance.

## Performance opportunities, separated from proven improvements

1. Measure native bulk against the same prepared, text-batch and LOCAL INFILE
   alternatives with identical rows, transactions, durability and verification.
   Do not transplant Haskell speed ratios into Go marketing claims.
2. Preserve existing metadata omission and the Go 1.27 scanner. The controlled
   32-column metadata benchmark changes only the server-advertised cache bit;
   it records bytes received, allocations and latency for equivalent SQL.
3. Investigate cached parameter type signatures: `writeExecutePacket` currently
   emits `newParameterBoundFlag=1` and all parameter types each execution.
   Handle NULL/type/unsigned changes, reset/reprepare and failed uploads before
   attempting to skip that data.
4. Remove the full-size long-data staging copy using bounded chunks. Lower live
   memory is a separate claim from total allocations or throughput.
5. Add explicit pool reset and measure the entire reset/restore/prepare/request
   boundary against equivalent reconnect/initialize/prepare/request work. Reuse
   must not preserve another request's user variables, roles or temporary tables.
6. Profile text-number conversion and RawBytes usage with Go 1.25/1.26/1.27
   separately. Microbenchmarks isolate decoding; live tests include server work,
   transport and database/sql. Avoid trading normal typed scans for retained
   borrowed buffers or unsafe string conversions.
7. Evaluate compression on a defined high-latency/bandwidth-limited profile.
   Local zlib cost is not evidence of a WAN speedup. zstd is not negotiated by
   this MariaDB runtime; inherited codec/mock tests are separate evidence.
8. Measure retained compression buffers after a large result followed by small
   queries. `compIO.buff` is a bytes.Buffer whose Reset retains capacity; this is
   separate from the normal buffer's 256 KiB reusable-storage cap. The unusually
   low allocation count for repeated compressed BLOB reads is consistent with
   reuse, but this pass has not measured the retained-heap cost. Add a heap
   experiment before choosing a shrink/reuse threshold.

## Initial measured snapshot

All **184 profiles** completed **seven repetitions**. The original 37 production
Go/module inputs still match the source commit, and executable/workload hashes
were stable throughout. The ordinary leaf total is **865 PASS / 79 FAIL / 7 SKIP**:
658 inherited passes, two protocol passes and 205 added live passes. The 76
protocol failures are predominantly truncation variants; the other three are
the unsigned ID, PARSEC and desired clean-pool assertions. The race run repeats
863 passes and those same three failures, with no race-detector warnings. Vet
and all four recorder parser tests pass. Correctness exits nonzero honestly.

Selected **existing API alternatives**, not new fork improvements:

| Equivalent workload / boundary | Alternative | Existing faster path | Result |
| --- | ---: | ---: | --- |
| 1,000-row, 32-integer-column live read | Text typed scan: 2.715 ms | Prepared typed scan: 0.496 ms | About 5.5× throughput; combines binary encoding, metadata handling and Go 1.27 scanning, not an isolated cache effect |
| 32-column prepared scalar, cache control | Uncached: 33.527 µs, 1,072 wire bytes | Cached: 31.077 µs, 155 wire bytes | 7.3% less time, 85.5% fewer received bytes and 49.3% fewer allocated bytes |
| Equivalent 100-row transactional replacement | Sequential prepared: 3.607 ms | Text batch: 0.444 ms | About 8.1× throughput; native binary bulk remains unimplemented |

The user confirmed an idle window, but a late snapshot found concurrent
virtualization activity. These are the initial retained measurements, not a
verified contention-free performance acceptance run. No favorable timing
confirmation was selected or baseline overwritten. The
[live report](../validation/RESULTS.md) contains every profile, and the immutable
baseline includes all raw logs/samples. The current inventory corrects one
descriptive PARSEC plugin label using the captured error; no measured value or
outcome changes.

## Explicit limits of this pass

The real profile is one server version, one Mac architecture and Go 1.27.4.
performance_schema is disabled on the shared service; we do not enable it or
change durability/max_connections. Inherited saturation stress is disabled
because it deliberately consumes the server's whole connection budget; the new
bounded pool test runs instead. All skips are retained with their original text.

Still needed: isolated oldest/LTS/current MariaDB profiles, Go 1.25/1.26,
Linux/Windows/ARM/x86 coverage, independent Connector/C controls if useful, real
PAM/Kerberos fixtures, deadlock/lock-timeout and disconnect-during-COMMIT proxies,
multi-packet boundary/fuzz campaigns, malformed compression controls, typed
metadata checks and fresh-chunk heap-retention tests. See the
[staged plan](ROADMAP.md). No claim of exhaustive MariaDB certification is made.
