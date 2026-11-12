# MariaDB Go coverage map

**Stage 3 review fixes, 12 November 2026:** Added cursor materialization-error
identity/reuse (including legacy/compressed/cached modes), direct/wrapped callback
ErrBadConn identity on SQL success/failure, explicit/autodetected stream packet
limits and exact rollback data assertions. Full-run publication shares strict
fixture/command/raw-log checks with its verifier and probes the measured endpoint
before/after. Regression negative controls and receipts are linked from the
[fix record](../docs/STAGE3_REVIEW_FIXES.md).

**Native APIs, 12 November 2026:** Per-unit bulk counters, one-shot reader uploads,
read-only server cursor Rows and per-command progress callbacks now have live
tests across TCP, zlib, TLS, Unix sockets, X.509, legacy EOF and uncached metadata.
Fault tests cover malformed unit/progress/fetch responses and cancellation during
upload, execute, fetch, drain and reset. Reader allocation is bounded against a
materializing negative control. Real ALTER progress covers successful callbacks,
callback errors/panics and terminal server-error preservation; deterministic peers
require joined callback/server errors when a report precedes failure. No progress
report is guaranteed for commands that fail early. See the
[contracts and initial evidence](../docs/STAGE3_NATIVE_APIS.md). That initial
receipt ran correctness only; the later [benchmark follow-up](../docs/STAGE3_BENCHMARKS.md)
is recorded separately. Later rows below describe the historical initial review.

**Session review corrections:** Retained/discarded extended metadata share
attribute validation and fixed-tail checks, with prefix, duplicate/count, budget,
EOF and malformed-stream retirement regressions. Publication tests reject
substituted or narrowed check commands, missing server provenance, raw/summary
mismatches and server-setting drift. Revision lookup covers absent Git and
unavailable repository metadata. See the
[correction record](../docs/STAGE2_REVIEW_FIXES.md) for final-source results.

**Session foundations, 12 November 2026:** Opt-in reset/restoration, statement
generations, bounded owned session observations and extended type attributes now
have persistent mock/live coverage. Tests include real rollback/private-state
cleanup, role-only schema access, repeated prepared reads/writes, native bulk,
TCP/zlib/TLS/socket/X.509/legacy EOF and uncached metadata, DDL reprepare, phase
cancellation and failed-restoration retirement. The original desired isolation
assertion explicitly opts into managed reuse; ordinary checkout is tested
separately. See the [current correctness and older timing sources](RESULTS.md)
and [implementation limits](../docs/STAGE2_SESSION_FOUNDATIONS.md). No performance
measurement was run for this stage. The following snapshots are historical.

**Review corrections, 12 November 2026:** Persistent regressions now cover
checked EOF/OK draining, malformed-row retirement, cleanup cancellation under
a stalled peer, legacy EOF with cached metadata, bulk date parity, actual BLOB
compression and native errors/cancellation across TCP/zlib/TLS/socket/X.509.
Phase-fault tests cover prepare write/reply, execute write/reply and close write;
rollback and healthy-session reuse after application conversion errors also pass.
The current source has 1,018 unique Mac passing leaves and one desired session-reset gap.
The exact-source Linux Go 1.27.4 run has 1,019 unique passing leaves and the same gap; workflow
configuration for Go 1.25/1.26 is not evidence those hosted jobs have run.
All 24 recorder/CI-policy checks pass, both fuzz campaigns pass, and all 200
profiles retain seven samples. See the [correction record](../docs/STAGE1_REVIEW_FIXES.md)
for the historical parser count clarification, the later correctness-only race
confirmation, skips and open performance acceptance. Earlier coverage tables
below retain their historical scope.

**Dedicated-driver follow-up, 11 November 2026:** The original coverage table
below describes the unchanged-driver stage. Current additions include parser/
receive/auth/metadata limits, PARSEC signature and live authentication, unsigned
result extensions, native aggregate bulk at 1/16/100/1000 rows, NULL/DEFAULT/IGNORE,
validation-before-execute and sixteen matched bulk benchmarks. See the
[implementation record](../docs/DEDICATED_DRIVER.md) and current capability
inventory. Managed reset and the later APIs remain open. The baseline assertions
and historical gaps are preserved rather than retrospectively rewritten.

11 November 2026. Exact results and skips: [live report](RESULTS.md). Driver
implementation is unchanged; no unsupported capability is recorded as a PASS.

| Area | Executable coverage in this pass | Remaining work |
| --- | --- | --- |
| Wire framing / metadata | inherited packet/row/compression tests; truncated greetings, OK, cached headers and columns; omitted cache; receive-budget probe | split-message cumulative budgets, bounded auth/metadata/decompression, wider fuzz campaigns and real disconnect proxy |
| Values | 24 expressions plus NULL/empty, six paths; signed/unsigned extremes, exact decimals, binary/NUL, Unicode, dates/microseconds/negative TIME, JSON/UUID/INET/VECTOR/geometry expressions | native table type identity, all precision limits, additional charset/domain Scanners |
| Binding | prepared/interpolated/compressed parameters, uint64 max, Unicode/escaping/NUL/injection-shaped values, NO_BACKSLASH_ESCAPES, 2 MiB length/hash validation; inherited long-data tests | incremental io.Reader contract, partial upload faults, retained heap/negative control |
| SQL features | recursive/nonrecursive CTE, windows, JSON_TABLE, sequence engine/objects, INSERT RETURNING, system versioning, Oracle mode | broader DDL/SQL-mode interactions and server-version profiles |
| Results | text multiple result sets, repeated prepared procedure with different result shapes, DDL reprepare, unsigned insert ID | OUT parameters/native bulk units, lost cache/type changes, failure between result sets |
| Transactions | commit/rollback/savepoint, read-only, read committed/repeatable read/serializable | lock timeout/deadlock, disconnect around COMMIT and durable uncertainty/replay tests |
| Ownership / cancellation | cancellation with fresh connection, early result draining, owned byte slices, bounded concurrent pool, server error number/SQLSTATE | repeated cancellation at each phase, slow cleanup and transport faults |
| Session isolation | explicit desired clean-checkout assertion exposes retained user variable | opt-in reset, role/schema/charset/time-zone/SQL-mode restoration, temporary tables and tracker payloads |
| Authentication / TLS | real native, ed25519, PARSEC attempt, SHA-2, Unix identity, verified TLS/X.509, bad password and bad hostname | isolated interactive PAM/GSSAPI, auth limits/deadlines, expiry/revocation/rotation and continuation fuzzing |
| Bulk / cursors / progress | source-reviewed missing API inventory; existing prepared/text/LOCAL INFILE alternatives benchmarked | real APIs, correctness/fault tests and separate feature benchmarks |
| CDC | source-reviewed absent API only | optional separately scoped binlog/GTID transaction/checkpoint package |
| Toolchain / static | Go 1.27.4, vet, race-enabled inherited/live suite, four recorder parser tests | Go 1.25/1.26, multi-OS/architecture CI and staticcheck version profile |
| Performance | 144 inherited + 40 added profiles, seven repetitions; live scans, metadata wire-byte control, batch alternatives, connections/pools, decoder/scanner microbenchmarks | native bulk/reset/cursors/streaming/auth extension baselines; network-emulated WAN; optional independent C control |

The inherited saturation test intentionally tries to consume all server
connections; it is skipped on the shared service. Its replacement here exercises
eight workers and a four-connection pool, without claiming saturation coverage.
performance_schema is OFF. External PAM/Kerberos services and multi-node server
topologies are not installed. These are explicit fixture gaps, never feature PASSes.
