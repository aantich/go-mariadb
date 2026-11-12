# Stage 6: optional MariaDB change-data capture

Authorized 12 November 2026, parent `b3300c2`. Implemented as an optional
`github.com/aantich/go-mariadb/cdc` package. Readiness remains **Alpha**.
Benchmarks remain deferred, and this stage stays uncommitted for review.
All seven subsequent review findings are addressed in the
[review correction record](STAGE6_REVIEW_FIXES.md), with newer exact-source
1,588 Mac / 1,591 Linux passing results. The original confirmation below is
historical; current results are linked from the README.
See the [usage and durability guide](USER_GUIDE.md#change-data-capture-cdc).

The `cdc` package uses a fresh dedicated authenticated replication connection,
never a borrowed `database/sql` connection. It reuses existing TLS, compression
and authentication policies. Reading is pull-driven, with independent bounds on
wire/expanded event bytes, retained table metadata, columns and decoded cells.
Returned events own their storage. No transaction-sized read-ahead is required.

Checkpoints are issued only after complete event groups. They retain the full
completed MariaDB domain/server/sequence state when opened from GTIDs; file
starts retain exact coordinates instead of inventing preceding GTID history.
Persist destination writes and resume state atomically before acknowledging.
Acknowledgement validates stream ownership and monotonic delivery. Optional
semisync acknowledges source receipt and does not establish exactly-once
destination delivery. Reconnect is explicit from the durable state; no hidden
retry advances progress or replays application SQL.

MariaDB format-4 events, CRC32/OFF checksum transitions, rotations, annotations,
table maps, sparse insert/update/delete rows, transaction boundaries, GTIDs and
MariaDB compressed events are in scope. Unknown non-ignorable events, XA and
two-phase ALTER groups fail before a checkpoint can advance. Unsupported
storage-engine-specific replication modes remain explicit rather than treated
as ordinary binlog events. Column names/signedness come from binlog metadata,
never a catalog query against a potentially newer table definition.

Protocol references: the checked-out MariaDB `sql/log_event.{h,cc}`,
`sql/sql_repl.cc`, `sql/semisync_master.cc`, and the
[MariaDB replication protocol](https://mariadb.com/docs/server/reference/clientserver-protocol/replication-protocol).
The Haskell driver's existing CDC implementation supplies prior project knowledge;
the Go implementation uses its own ownership and cancellation contracts.


## Connection, ownership and memory

`cdc.Open(ctx, cfg, options)` authenticates on a fresh connection and sets only
session-local replication capabilities, GTID connect state, checksum negotiation,
heartbeat and semisync variables. The source must be MariaDB. SQL pool behavior,
ordinary packet sequencing and retry rules are unchanged. Cancellation/error
retires the replication connection; reconnect from durable state explicitly.
`Close` interrupts I/O without sending COM_QUIT during replication.

The transport bounds cumulative logical message bytes before reading an offending
body, including physical packets larger than 16 MiB. Split fragments must have
contiguous sequence numbers. MariaDB semisync/compression may reset sequence
numbers between logical events; that exception is confined to replication.
An ACK uses separate compressed output state and never resets buffered unread
input. Operations serialize, but waiting cancellation does not poison the active
reader. Progress getters return owned token copies without waiting on the socket.

Defaults are 64 MiB per wire/expanded event, 4096 retained table maps, 16 MiB
conservatively charged table state, 4096 columns and 1,048,576 cells per event.
Sparse absent cells count toward the limit. Table generations are cleared at
statement/group boundaries, rotation and format changes. Events own their bytes;
retained metadata and emitted table snapshots cannot alias mutably. A consumer
can stop pulling to apply backpressure. Application-retained events and sink
transactions require application-level memory limits.

Cells retain exact value bytes and convert lazily. Packed DECIMAL, temporal and
BIT encodings are nevertheless validated before a complete-group checkpoint can
be delivered. Optional binlog metadata supplies names and signedness. Unknown
signedness remains explicit; no query against a newer schema is used. Integer,
floating, exact decimal, bit/enum/set/year and temporal decoding is available.
Strings/blobs, JSON, UUID/INET, VECTOR and geometry remain raw bytes with the
recorded metadata for consumer interpretation.

## Progress and failure contracts

`Resume` is JSON serializable and holds either exact file coordinates or full
completed MariaDB domain/server/sequence progress. The explicit `UseGTID` flag
keeps empty history distinct from invalid zero file coordinates. GTID states are
immutable, canonical and bounded to 4096 domains/65,535 text bytes. Constructors,
advances and JSON restoration share the same limits. String formatting uses a
cached exact length and stack numeric formatting; group completion reuses its
already validated immutable candidate instead of copying the domain set twice.

`Checkpoint` is opaque and stream-local. Complete XID/COMMIT/ROLLBACK and
standalone groups can issue it; intermediate rows, GTIDs, annotations, heartbeats
and rotations cannot. A truncated or invalid group never advances progress.
Tokens may be copied, but missing/foreign/decreasing tokens cannot ACK. Latest
observed and consumer-acknowledged progress remain separate. Neither implies
external persistence. The consumer stages destination work and atomically commits
that work with `checkpoint.Resume()` before acknowledging. There is no automatic
checkpoint file writer or exactly-once guarantee.

With semisync, ACK sends an explicit file/position source receipt only after
application persistence. Failed ACK does not advance local acknowledged progress.
ACK before reading onward to EOF or blocking on a subsequent event. Reconnect is
explicit; purged history, permission denial, cancellation and malformed input are
terminal, never permission to skip to the source's current position.

MariaDB reserves an FDE footer even with checksum OFF. Its replication path can
rewrite the sent OFF format header without updating that footer. The decoder
uses the declared checksum algorithm: CRC32 files require a valid CRC (including
FDE), whereas OFF FDEs strip the reserved footer without interpreting it as CRC.
Subsequent OFF events have no checksum. No CRC failure silently downgrades mode.

## Executable coverage

The private native fixture uses an owned temporary datadir/socket, no TCP
listener, ROW/FULL metadata, a private TLS CA and the semisync plugin. Rotations,
global compression/checksum settings, purges and restart controls affect only
this fixture. Its child runs in an owned process group with bounded termination;
server shutdown is separately bounded. No host PAM/realm or shared MariaDB
configuration is changed. Authentication wraps CDC so both fixtures reap their
own resources on failure.

Fifteen real-server cases cover cursor-enabled Config reuse; plain, zlib, verified TLS and combined transport;
FULL/MINIMAL row images; MariaDB event compression; numeric/decimal/temporal and
MariaDB UUID/INET/VECTOR/JSON/geometry families; CRC32/OFF changes; a 17 MiB split
event in plain/compressed modes and pre-body receive rejection; durable GTID
resume across rotation/DDL; idle cancellation; two semisync writes held until
consumer ACK; server restart/resume; purged coordinates and denied privileges.

Deterministic tests cover v1/v2 insert/update/delete images, rollback/standalone
boundaries, stale table IDs, mutable output snapshots, bad CRC/header/metadata,
unknown critical events, XA/two-phase ALTER, limits, compression expansion/trailing
bytes, fragment ordering/truncation/interleaving, malformed scalar values before
checkpoint, token ownership/monotonicity, cancellation/Close/ACK errors and
concurrent ownership waits. A buffered-compression regression checks that ACK
preserves already-read input. Public examples compile with the suite.

The harness now requires current CDC fixture provenance in both capture metadata
and raw Go events. Missing either, missing both, duplicates, wrong settings or a
fixture source mismatch fail both correctness and full publication validators.
The future full capture contract is `full-v3`; no timed workload was changed.
All historical receipts and original/native-bulk baseline files remain immutable.

## Original confirmation, 12 November 2026

This is the preserved pre-review result. The
[review corrections](STAGE6_REVIEW_FIXES.md) supersede it for current source.

Source fingerprint: `f1ae6b97668f1fd10f61af0b1483649db49093cfec67713ce8fa9fef92e5370b`
(68 driver/native inputs, 101 workload/harness inputs).

| Profile | Passing leaves | Failures | Skips | Race / vet / harness |
| --- | ---: | ---: | ---: | --- |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,504 | 0 | 8 | PASS / PASS / 82 PASS |
| isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,507 | 0 | 5 | PASS / PASS / 82 PASS |

All 1,398/1,401 Stage 5 passing leaves survive, with 106 new leaves per profile.
The 8/5 inherited/profile skips are retained; no CDC fixture or required test
skipped. The final repeat also preserves all 1,501/1,504 passes from the first
complete Stage 6 attempt. Mac instrumentation remains a named profile skip;
Linux requires real instrumentation.

Both 8-second correctness fuzz campaigns pass: 155,232 decoder and 342,336 cell
executions on final source. Five isolated, race-enabled defect restorations fail
the intended tests (CRC bypass, premature checkpoint, destructive compressed
ACK, TIME2 reserved-bit masking and metadata-accounting overflow). They are valid
compiled controls, not build failures. Current provenance-omission controls run
inside the 82-test harness. CGO-disabled provider/CDC tests, Windows amd64
root/CDC cross-compilation, Linux 386 CDC cross-compilation, actionlint and
format/diff checks pass. Windows/386 runtime coverage is not claimed.

The final [Mac receipt](../validation/results/stage6-mac-edge-final/capture.json),
[Linux receipt](../validation/results/stage6-linux-edge-final/capture.json) and
[confirmation manifest](../validation/results/stage6-confirmation.json) retain
commands, source hashes, raw logs, fixture provenance, prior-pass snapshots and
artifact hashes. The [live report](../validation/RESULTS.md) now publishes current
correctness separately from earlier timings. All development attempts, including
failed fixture/decoder experiments and the first green source, remain under
`validation/results/stage6-20261112` and the earlier Stage 6 receipt directories.
Those attempts exposed sequence resets, OFF-FDE handling and DECIMAL formatting;
the final review additionally tightened TIME2, post-headers and overflow checks.

No benchmark functions, smoke checks or profiling workloads ran. The timing
pointer and immutable references remain byte-for-byte unchanged. Private fixture
servers and the disposable Linux container were reaped. Nothing was committed,
pushed or released.

## Scope still requiring further work

* XA and two-phase ALTER are rejected before progress advances. Supporting their
  prepared/recovery semantics requires a separate checkpoint contract.
* Storage-engine-specific binlog/GTID semisync protocols and unknown critical
  events/types are unsupported. Traditional file starts are limited to uint32;
  GTID resume is preferred. This is not a general MySQL binlog parser.
* Partial-row event 172 from MariaDB 13.2 is source-reviewed and tested with
  synthetic events; the live fixture is MariaDB 12.3.3. Older/newer versions,
  large production topologies, failover and real application sinks need pilots.
* Snapshot coordination, destination application/DDL policy, durable storage,
  idempotency and external buffering belong to the consumer.
* No CDC throughput, latency, allocation profile or speedup is claimed. Existing
  200-profile measurements remain historical; the 62 Stage 4 profiles and CDC
  performance work await an authorized measurement window.
