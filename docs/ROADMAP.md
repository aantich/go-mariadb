# MariaDB Go driver implementation roadmap

**Current decision, 11 November 2026:** Anton declined upstream PRs and authorized
a dedicated MariaDB driver. The [first implementation stage](DEDICATED_DRIVER.md)
adds independent identity, protocol/result hardening, PARSEC and aggregate native
bulk. The [live report](../validation/RESULTS.md) records measured results and
remaining failures. Anton approved committing and pushing the completed stage
on 12 November; no package release is authorized.
Earlier proposal/authorization statements below describe the historical review.

**Session foundations, 12 November 2026:** Opt-in reset/restoration and statement
generations, bounded session observations, extended native metadata and isolated
instrumentation are implemented in the working tree. See the
[contracts and correctness receipt](STAGE2_SESSION_FOUNDATIONS.md). No benchmarks
were run for this stage; previous timing samples remain historical.

The [session review corrections](STAGE2_REVIEW_FIXES.md) address all four review
findings, preserve the original references and add exact-source Mac/Linux and
source-export correctness receipts. Performance acceptance remains deferred.

**Native APIs, 12 November 2026:** Anton approved per-unit bulk results, streamed
parameters, server cursors and progress callbacks. Their implementation and
correctness evidence are recorded in [Stage 3](STAGE3_NATIVE_APIS.md). The new
work remains uncommitted for review. Anton later authorized the
[full existing-suite benchmark follow-up](STAGE3_BENCHMARKS.md); references remain fixed.

The [Stage 3 review](STAGE3_CODE_REVIEW.md) identified six corrections:
cursor SQL errors, stream packet boundaries, callback error identity, full-run
check/provenance validation and missing rollback assertions. Anton authorized
[all six fixes](STAGE3_REVIEW_FIXES.md), now implemented with persistent
regressions and newer correctness receipts. Anton subsequently authorized commit
and push of the complete stage on 12 November. Timing captures and immutable
references are preserved; publication does not repeat measurements.

**Reusable native statements, later on 12 November:** Anton approved Stage 4
implementation but excluded all benchmarks while using the Mac. Explicit
`PrepareNative` bulk/unit/stream reuse, regression coverage, 62 additive benchmark
profiles and dry-run profiling tooling are implemented.
See the [contracts, coverage and measurement limits](STAGE4_STATEMENT_REUSE.md).
The original 200 timing profiles/references remain unchanged; no Stage 4 timings
or accepted speedup exist. Read-path changes requiring profiles remain deferred.

The [four Stage 4 review corrections](STAGE4_REVIEW_FIXES.md) are implemented:
deferred close respects active Rows after pool return, newer correctness passes
have an independent history reference, unit benchmarks reject missing results,
and diagnostic exceptions retain evidence. Matching-source Mac/Linux checks
pass. Anton subsequently authorized commit and push of the complete stage and
corrections to `master`. Saved receipts retain their original prepublication
context; no tests or measurements are repeated for publication. The new profiles
remain unmeasured.

**External authentication, later on 12 November:** Anton requested implementing
the next functional stage while deferring benchmarks. [Stage 5](STAGE5_AUTHENTICATION.md)
adds opt-in PAM dialogue and principal-pinned GSSAPI with isolated real services.
Performance-dependent read-path optimization and pipelining remain deferred.

All three [Stage 5 review](STAGE5_CODE_REVIEW.md) findings are
[corrected and validated](STAGE5_REVIEW_FIXES.md): secrecy across auth switches,
native work bounded through cleanup, and required current fixture provenance.
Matching-source Mac/Linux correctness passes with all earlier passes preserved;
no benchmarks ran. Anton subsequently requested committing and pushing the full
stage and saved evidence to `master`. Earlier uncommitted statements describe
their original capture context; publication does not repeat measurements.

**Next proposed functional stage while measurements remain deferred:** an
optional MariaDB CDC/binlog package, kept separate from ordinary `database/sql`
connections. Start with an explicit event/transaction/checkpoint contract,
bounded row-event decoding and a dedicated replication connection, then add
consumer-controlled checkpoints and GTID resume with disconnect/replay tests.
Do not promise exactly-once delivery. This is a proposal, not authorization to
implement or publish a CDC package. The performance runbook stays queued for an
explicitly authorized measurement window.

Next performance work: authorized first measurements for the new profiles and attribution of the
remaining text-read signals. Evaluate pipelining independently after that,
followed by optional CDC as a separately scoped feature. Production PAM/Kerberos
deployments and broader platform/provider integration remain open validation work.
Anton subsequently requested preparation only, with no benchmarks yet. The
[measurement runbook](STAGE4_MEASUREMENT_PLAN.md) records the checks, comparison
rules and commands for a later authorized run; live results remain unchanged.
The remaining stages and
broader platform coverage remain open; this is not a claim they are complete.

Proposed 11 November 2026, based on the unchanged driver and
[initial review](MARIADB_REVIEW.md). The current authorization covers inspection,
test/benchmark infrastructure and the baseline. Driver changes below are a plan
for review, not completed work. No commit/push or package publication is implied.

**Distribution checkpoint, later on 11 November:** Anton requested assessing
upstream contributions before fixes. The [upstream/package assessment](UPSTREAM_STRATEGY.md)
recommends contributing focused fixes and performance work upstream while keeping
this checkout for development. Several parser/auth/reset/metadata proposals
already exist. Review those first; agree the native bulk API before choosing an
independent public driver. This is a recommendation, not approval to submit PRs,
contact maintainers, rename the module or implement the stages below.

## 0. Establish and preserve the starting point — this pass

* Keep production driver code and inherited tests unchanged at `e623245`.
* Record inherited live tests, added MariaDB assertions, deterministic malformed
  peer probes, race detection, vet, skips, unsupported APIs and fixture gaps.
* Freeze seven repetitions of all inherited benchmarks and 40 new profiles.
  Preserve raw logs, module versions, exact source/workload hashes, machine,
  server configuration, compiler and the user-confirmed measurement conditions.
* Publish a live Baseline / Current / Change table linked from the README.
  Protect every original PASS separately. A failing baseline is useful evidence.
* Keep workload bodies immutable. Add new feature tests/benchmarks in new files
  with independent first baselines; do not retrofit new work into old samples.

**Completed:** [initial baseline](../validation/RESULTS.md), source review,
capability inventory and 184 profiles × seven repetitions. Production inputs
remain unchanged; all baseline checksums and freshness verification pass.
Correctness deliberately remains red with the identified findings. A late
background-VM observation qualifies the user-confirmed idle window.

Before accepting later performance comparisons, explicitly verify hardware and
the resolved external module graph as well as the recorder's OS/compiler/server
checks. Record a separate profile for changed hardware/dependencies. When module
identity changes, use a narrowly verified import/identifier metadata adapter;
preserve frozen workload bodies and original samples.

## 1. Harden the existing fast paths first

**Outcome:** malformed packets return bounded errors; successful ordinary queries
retain their existing throughput and allocation behavior.

1. Add checked length-encoded and fixed-tail decoding for greetings, OK packets,
   result headers, columns and prepare responses. Validate sizes/overflow before
   slicing or allocation, including metadata omission and schema-count mismatch.
2. Add distinct incoming logical-message, metadata/column and authentication
   budgets, plus bounded decompression. Keep outbound maxAllowedPacket semantics
   explicit. Check auth limits before reading bodies; avoid mutable auth-phase
   checks in every ordinary row/field operation.
3. Retire an unsynchronized connection on protocol failure. Preserve original
   context cancellation and server error identity, with redacted diagnostics.
   Extend deterministic truncation cases with fuzz seeds and short fuzz runs.
4. Preserve unsigned OK counters/IDs internally. Standard signed result methods
   return a clear overflow error; an additive unsigned interface exposes the full
   value. Validate both RowsAffected and multi-statement result lifetimes.
5. Validate cached metadata identity/count and invalidation after prepare,
   DDL/reprepare, reset, Exec/Query alternation and stored-procedure result sets.

**Acceptance:** every original passing assertion remains passing; parser and ID
findings are fixed; race/static checks pass. Full frozen benchmarks and decoder
microbenchmarks show no accepted regression. Investigate repeated >5% changes
with matching environment and raw samples; do not silently advance references.

## 2. MariaDB-native batch execution — highest expected throughput payoff

**Outcome:** ingestion/batch updates use MariaDB's native binary bulk protocol.

* Extend the existing high-word negotiation for bulk and unit results. Gate on
  actual server capabilities, not only a version string; document unavailable
  capabilities explicitly rather than silently switching semantics.
* Evaluate prepare/execute pipelining as a separate, earlier performance patch.
  Upstream already has a feature request and maintainer interest. It can reduce
  transient statement round trips without requiring the full bulk public API;
  validate error draining, cancellation and retry safety independently.
* Design an additive extension that fits database/sql connection ownership.
  Pin an sql.Conn for the operation, keep statement/cache lifetimes explicit and
  cancellation-safe, and avoid hiding commands inside unusual parameter values.
  Review the public API before implementation; optional native helpers should
  not require replacing the standard database/sql application API.
* Encode bounded batches with checked row/type/parameter counts, signed/unsigned
  values, NULL/DEFAULT/IGNORE indicators and aggregate/per-unit results. Validate
  all input before any partial send. Define empty batches and partial failures.
* Measure sequential prepared, text batch, LOCAL INFILE and native bulk at 1,
  16, 100 and 1,000 rows. Keep data, transaction boundaries, verification and
  durability equivalent. Include duplicate keys, nullable/default columns,
  upserts, mixed types, server errors and cancellation between protocol phases.
* Investigate parameter-type reuse separately after correctness. Invalidate
  signatures on type/unsigned changes, reprepare/reset and upload failure.

**Acceptance:** real protocol tests and fault tests, no lost ordinary passes,
equivalent results/atomicity and measured reduction in round trips/allocations.
Do not promise Haskell's measured ratios for Go.

## 3. Clean session reuse and native type information

**Outcome:** external pools can request verified clean reuse; applications can
inspect MariaDB type information without losing ordinary value compatibility.

* Add an explicit opt-in reset API/policy using COM_RESET_CONNECTION. Existing
  liveness-only checkout behavior must not silently become an expensive reset
  or erase intentionally persistent application session state.
* Restore configured charset/collation, role, schema, SQL mode, time zone and
  initialization hook in a specified order. Restore the encoding needed to quote
  identifiers, then the role before selecting a schema whose access it grants.
  Invalidate prepared handles, metadata
  and parameter signatures with a generation counter. Cleanup failure retires
  the connection, and a timed-out connection is never revived by reset.
* Negotiate/parse/expose bounded session-state tracking and extended metadata.
  Distinguish observed facts from assumptions after opaque SQL or errors.
* Test two-request isolation for user variables, temporary tables, transactions,
  roles, charset and initialization failures. Benchmark full equivalent request
  boundaries over TCP, sockets and verified TLS, at realistic pool concurrency.
* Add actual UUID/INET/JSON/VECTOR/geometry table metadata checks. Keep raw bytes
  available and use optional Scanner/Valuer helpers for domain types rather than
  forcing allocations and conversion on all callers.

## 4. Bounded large-data APIs and MariaDB authentication

* Introduce one-shot io.Reader prepared parameters with bounded chunk buffers,
  short-read/error handling and precise reset/retirement after partial uploads.
  Test fresh generated chunks and a buffering negative control; report retained
  heap, total allocation and elapsed time as different quantities.
* Add server cursor fetch with bounded batches, explicit lifetime/close rules,
  early stop and repeated cancellation tests. Batch size is a resource/latency
  tradeoff; cursors do not automatically make local reads faster.
* Add progress callbacks with explicit non-reentrancy and callback error/panic policy;
  preserve the final server error when callbacks fail. Negotiate the capability
  only when the caller opts in.
* Review/test upstream PARSEC PR #1827 and raw-challenge PR #1825 before adding
  duplicate implementations. The per-exchange auth interface exists on master,
  but is absent from the latest published v1.10.1 tag. Test against the actual
  server, including a binary challenge ending in zero. Keep ed25519 and MariaDB's supported SHA-2
  paths; shared protocol names containing mysql are not grounds for deletion.
* Add policy-controlled PAM dialogue and principal-pinned GSSAPI with isolated
  real fixtures. Evaluate an optional GSSAPI subpackage/plugin to preserve the
  core driver's pure-Go/no-C dependency property; document OS support honestly.

## 5. Compatibility, packaging and wider validation

* Resolve the distribution checkpoint before packaging. Our development target
  is MariaDB; upstream contributions must preserve upstream's MySQL support.
  Only for an independent driver, remove MySQL/TiDB server CI targets once
  MariaDB coverage replaces them; retain wire identifiers MariaDB
  itself uses. Keep upstream attribution and MPL-2.0 notices unchanged.
* Decide the Go module path and SQL driver registration name before public
  adoption. Changing module identity is a migration decision, not a speed fix.
* Run isolated MariaDB 10.11/11.4/11.8/12.3 and current-development profiles,
  constrained by the API's minimum server versions. Use separate baselines.
* Exercise Go 1.25/1.26/1.27 and Linux/macOS/Windows where actually supported.
  Preserve legacy and Go 1.27 scanning behavior; key CI caches by OS/architecture,
  compiler and dependencies. Separate infrastructure cancellations from failures.
* Add lock/deadlock, server restart, WAN/compression and disconnect-before/after
  COMMIT tests. Never return a retryable ErrBadConn after a potentially executed
  write merely to get convenient automatic retries.
* Expand user/migration guides, API comments, examples, release checks and
  application pilots before changing Alpha readiness claims.

## 6. Optional CDC package — separate scope decision

Only after the SQL-driver foundations: consider an optional MariaDB binlog/GTID
package with bounded decoding, complete-transaction checkpoints, durable resume,
consumer-controlled ACKs, checksum/compression validation and explicit unsupported
event handling. Reuse protocol knowledge from Haskell but design idiomatic Go
stream ownership and cancellation. Do not couple core SQL-driver adoption to CDC.

## Operating rules

Leave this pass uncommitted for review. Preserve both original source and failed
assertions. The comparison runner hashes the entire actual driver and harness;
the freshness check rejects changed inputs. New profiles or machines never
replace the original baseline. Record any source change, failure, limitation or
test-contract correction explicitly. Do not repeatedly rerun measurements to
select favorable timing samples.
