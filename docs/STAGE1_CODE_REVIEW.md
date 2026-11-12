# Dedicated Go MariaDB driver: implementation review

**Follow-up, 12 November 2026:** Anton authorized fixing all findings. The
[correction record](STAGE1_REVIEW_FIXES.md) documents the implementation and
final validation. The findings and reproductions below describe the reviewed
pre-correction fingerprint and are retained as history.

12 November 2026. Review requested by Anton; implementation fixes are not part
of this pass. The review covers the current uncommitted dedicated-driver stage,
the surrounding connection/statement/row/authentication/compression code, new
tests, CI and result-recording tools. Pure package/import renames were separated
from behavioral changes. The checked-out MariaDB server implementation was used
to verify the cached-metadata/EOF protocol.

**Conclusion:** five driver defects, two validation gaps and one report-generation
defect remain. Three driver defects are introduced in the new bulk/statement
changes; the EOF panic and row cleanup problems are inherited paths that the
hardening stage has not yet closed. Fix these before accepting the stage or
building more APIs on it.

Reviewed source/workload fingerprint:
`901628d5decc288cc479da04740fd57c5fc7f2a95526b4c0c32d3f89c9c61c1f`.
All 41 production/module inputs in the isolated review copy match the measured
working tree. No production code, frozen workloads, reference artifacts or live
benchmark results were changed. Review-only tests and logs are kept separately
in [review evidence](../validation/results/review-20261112/capture.json).

| ID | Priority | Finding | Evidence |
| --- | --- | --- | --- |
| R1 | P1 | Compressed native bulk reads its own outgoing buffer as a reply | Real MariaDB: wrong affected-row count and misaligned next query |
| R2 | P1 | Malformed EOF packets still panic while draining results | Ten deterministic truncation cases across both EOF modes |
| R3 | P2 | Cached-metadata `Exec` misses the legacy metadata terminator | Mock leaves 19 bytes unread; live next query fails with busy buffer |
| R4 | P2 | Bulk changes the meaning of `time.Time{}` | Real MariaDB stores year 0001 instead of the ordinary zero date |
| R5 | P2 | Malformed row cleanup can outlive the query deadline | Default read timeout plus stalled peer leaves Close blocked |
| R6 | P2 | Benchmark completeness guards accept a missing profile | Isolated 199-of-200 capture passes the actual verifier |
| R7 | P2 | CI skips the new live native APIs and tagged hardening tests | Missing fixture configuration causes successful skips |
| R8 | P3 | Regenerating the live report loses corrections and context | Isolated render drops the overview and restores a wrong workload caption |

## R1 — Synchronize compressed I/O before reading the bulk reply

**Location:** `bulk.go:94`, immediately after `writePacket` and before result
reading. Related implementation: `connection.go:94`, `compress.go:198`.

The bulk path writes directly through `mc.writePacket` but omits
`mc.syncSequence()`. Every ordinary command/statement path performs this
transition. Under compression it both synchronizes packet numbering and resets
the compression scratch buffer, which is shared between outgoing and incoming
traffic. Without it, reply parsing consumes bytes of the outgoing compressed
packet already held in that buffer.

`TestReviewBulkTransports` passes without compression. With zlib, the same
one-row insert returns success with **0 affected rows instead of 1**. The next
SELECT reports no rows, having consumed the pending bulk reply as its own result.
The log also records expected sequence 1 versus actual sequence 0. This is a
response-association defect, not simply a slow path or an unavailable capability.

**Correction:** apply the normal write/read transition after the successful bulk
write. Add native bulk correctness and subsequent-command checks across plain,
compressed, verified TLS and socket connections. Include errors and cancellation;
codec negotiation limits remain distinct from whether bulk honors compression.

## R2 — Complete EOF/OK bounds checking in the drain path

**Location:** `packets.go:931` in `skipRows`; `readStatus` at `packets.go:625`.

The checked ordinary OK parser does not protect result draining. `skipRows`
still slices `data[3:]` for legacy EOF and indexes status bytes without checking
their presence. Its deprecated-EOF branch also uses unchecked length-prefix
advancement followed by unchecked status decoding.

`TestReviewTruncatedEOFCannotPanic` supplies payloads `fe`, `fe00`, `fefc`,
`fe0000` and `fe000000` in each EOF mode. **All ten cases panic.** Recovery exists
only in the review test to record the failure; the driver itself does not recover.
These paths are reached by Exec result draining, unread-row Close and remaining
result-set cleanup. A malformed peer/proxy can therefore still terminate the
application process. Correct MariaDB output is not claimed to trigger this.

This code was inherited rather than introduced in this stage. It nevertheless
contradicts completion of the malformed-packet hardening goal.

**Correction:** use one checked terminator decoder for read and discard paths;
validate required status/warning fields and non-NULL integer encodings. Retire
malformed streams while preserving legitimate server errors and avoiding a
retryable error after a potentially executed write. Add truncation/fuzz coverage
for draining, not only greeting/column/ordinary OK parsing.

## R3 — Consume the metadata EOF even when column packets are omitted

**Location:** `statement.go:86`, before `skipRows`.

The new Exec path correctly avoids reading omitted column definitions, but it
also skips their terminator. With metadata caching enabled and
`CLIENT_DEPRECATE_EOF` disabled, MariaDB still sends a metadata EOF even when
`metadata_follows=0`. The Query path already calls `skipEof` in this situation.
Exec instead lets `skipRows` mistake the metadata EOF for the end of the rows.

The mock reproduction returns success with **19 response bytes unread**. A live
MariaDB reproduction negotiates legacy EOF by clearing that client capability
on the wire and in the review connection state. Preparing and executing
`SELECT 123` succeeds; the following `SELECT 456` fails with `busy buffer`.
The default local negotiation uses deprecated EOF, which explains why the
existing workload missed this valid capability combination. The reproduction
does not establish that ordinary default connections to every supported version
use legacy EOF.

The server's `sql/protocol.cc:1251` confirms that metadata EOF emission depends
on `CLIENT_DEPRECATE_EOF`, independently of whether column definitions were sent.

**Correction:** after validating the cached column count, consume the optional
metadata terminator just as Query does. Cover Exec/Query alternation, zero and
nonzero row counts, subsequent results and both EOF negotiations.

## R4 — Preserve ordinary zero-time parameter semantics in bulk

**Location:** `bulk.go:182`. Ordinary comparison: `packets.go:1174`.

Ordinary prepared execution special-cases `time.Time{}` before timezone
conversion and encodes `0000-00-00`. Bulk always converts through `date.In(Loc)`
and `appendDateTime`, producing `0001-01-01` in UTC. A negative-offset location
can instead move the zero value into year zero and fail date validation.

`TestReviewBulkZeroTimeParity` uses a connection-local temporary DATETIME table
and a permissive session SQL mode to allow a direct comparison. For identical
Go values, ordinary execution stores **0000-00-00 00:00:00** and bulk stores
**0001-01-01 00:00:00**. This silently changes data when replacing prepared loops
with the new bulk API. Strict modes may reject one or both values; they do not
make the encodings equivalent.

**Correction:** share the ordinary zero-time normalization before applying Loc
or truncation. Add bulk/prepared parity for zero time, non-UTC locations,
microseconds, truncation and invalid years.

## R5 — Retire malformed rows and keep cancellation active during cleanup

**Locations:** `rows_decode.go:35`, `rows_column_scanner.go:46`,
`packets.go:823`, and `rows.go:138`.

Row structure errors return `ErrMalformPkt` without retiring the connection.
`TestReviewMalformedRowMustRetire` supplies a truncated length-prefixed row:
decoding fails while `mc.closed` remains false. The new `protocolError` policy
has not been applied to these inherited legacy/direct-scanner paths.

There is a concrete cleanup consequence. Rows.Close calls its finish hook
**before** draining unread rows. That hook stops watching the query context.
With the default `ReadTimeout=0`, a peer that stalls after a malformed row leaves
the cleanup read without either a deadline or a live cancellation watcher.
`TestReviewMalformedRowCloseDeadline` uses an in-memory transport, a 50 ms query
deadline and no further peer response. Close remains blocked at 200 ms and only
returns when the test forcibly closes the transport. The test is deterministic
and leaves no blocked goroutine behind.

**Correction:** retire on structural row errors, consistently for legacy Next
and Go 1.27 NextRow. Also keep cancellation effective through ordinary draining
and finish it after cleanup, or retire when cleanup cannot complete safely.
Test early close and conversion/structural errors under a stalled peer with the
default timeout settings. Distinguish an application Scanner conversion error
from a wire-format error; do not discard healthy sessions for every Scan error.

## R6 — Check expected benchmark identities, not only surviving repetitions

**Locations:** `validation/record.py:275`, `validation/verify.py:29`, and the
current-only table iteration at `validation/record.py:173`.

The recorder requires nonempty output and seven samples for each discovered
profile. The verifier repeats the sample-count check. Neither requires every
profile in the original/native reference to exist in the current capture.
Consequently a benchmark skipped in all seven repetitions disappears from the
comparison and can still be called complete. Rendering only current keys hides
the absent row too.

The read-only recorder reproduction removes `BenchmarkQuery` from a temporary
copy of current.json. The actual verifier accepts the remaining **199 profiles**
and prints its normal freshness/success message. Real reference files and the
actual current capture were not modified.

**The actual measured capture contains all 200 required profiles.** This finding
does not invalidate its completeness; it exposes an unreliable future guard.

**Correction:** compare required profile sets from both immutable references;
require each expected family/profile/repetition identity and required finite
metrics. Reject or explicitly mark missing profiles and show them in the report.
Add a guard regression that omits an entire profile, not just one repetition.

## R7 — Make CI execute the MariaDB-specific suite

**Locations:** `.github/workflows/test.yml:104`, `validation/config_test.go:35`,
and the protocol file's `mariadb_validation` build tag.

CI only provisions `gotest`, never supplies `MARIADB_VALIDATION_CONFIG`, and runs
ordinary `go test ./...` without the protocol build tag. All new live validation
cases depend on that private configuration and therefore skip. The 78 tagged
protocol assertions are not compiled into that command. Root dedicated unit
tests do run; they are not a substitute for those live and protocol checks.

A local execution of the workflow's relevant environment reproduces successful
skips for native bulk, unsigned results and all live authentication variants.
Go even reports the authentication parent as PASS while all its child fixtures
skip. The workflow's benchmark command only targets the root package, so it does
not execute the sixteen new matched bulk workloads either. This is an inspection
of configured coverage plus a local skip reproduction, not a claim that remote
CI was executed in this review.

**Correction:** add an explicit fixture-backed MariaDB validation job with the
reserved schema/account configuration, tagged protocol checks and skip/failure
policy. Make TLS/plugin/socket limitations explicit per OS/server instead of
silently skipping required features. Keep the intended clean-session contract
gap visible rather than weakening its assertion to make CI green. Keep noisy
hosted-runner timing separate from accepted local benchmark references.

## R8 — Make live report explanations reproducible

**Location:** `validation/record.py:137`, especially the caption at line 170.

RESULTS.md has a manually added stage overview, performance acceptance status and
matched bulk summary. The renderer does not generate them. Its generic caption
also still states that reads consume 1,000 rows and writes replace 100 rows,
although inherited workloads have different units and native writes use
1/16/100/1000 rows. The current document was corrected manually; the next capture
restores the misleading caption and removes the overview.

The recorder reproduction renders into a temporary directory and confirms both
effects without touching the actual report.

**Correction:** generate the explanations and workload-specific units from
retained capture/analysis data, or keep maintained explanatory sections outside
the generated portion. Add a report-generation check covering the native sizes
and inherited workload units. Do not require manual corrections after every run.

## Performance and coverage assessment

No new timing campaign was run for this review. Existing benchmark evidence
remains relevant to the exact tested paths, subject to its recorded host limits:

* All 200 measured profiles have seven samples; both reference profile sets are
  present and frozen checksums still pass. The original passing assertions remain
  protected. Freshness verification is not evidence that untested combinations
  are correct, as the compressed-bulk reproduction demonstrates.
* Eighteen frozen original profiles exceed +5% time and eight exceed +20%; these
  remain unresolved acceptance signals. Original-driver diagnostic controls also
  drift, so neither code causation nor an environmental explanation for every
  slowdown is established. Diagnose compressed narrow reads and BLOB paths with
  matching interleaved controls and profiles before accepting regressions.
* The matched 100/1,000-row native speedups over prepared loops are supported for
  the measured uncompressed two-column workload. They do not validate compressed
  bulk, and text batching remains faster at 1,000 rows. Transient prepare/close
  and per-call conversion buffers are sensible later optimization targets after
  correctness, with explicit statement/connection lifetimes.
* Long-data staging is bounded at 64 KiB, which trades the old full-value copy
  for more write calls. Measure upload throughput and retained heap separately;
  do not infer either from successful buffered-parameter correctness checks.
  Compression scratch buffers still retain their high-water capacity; the
  ordinary 256 KiB buffer cap does not cap those buffers.
* The original native tests check NULL/IGNORE errors but do not read those values
  back. New review controls do read them back: NULL, DEFAULT and IGNORE all pass.
  An in-flight uncompressed bulk cancellation control also returns the original
  deadline error and confirms physical connection retirement. These successful
  controls narrow the findings rather than establishing full fault coverage.
* Add per-phase native prepare/write/reply/close faults, partial-server-error
  transaction/rollback cases, compressed/TLS/socket combinations, split logical
  budget boundaries, aggregate metadata/auth limits and decompression corruption
  to persistent regressions. Add files and independent first references; preserve
  the existing frozen workload bodies.

The desired clean-session reset assertion remains a known API gap. It is
separate from the defects found here. Per-unit results, reset/restoration,
extended metadata, reader parameters, cursors and the later roadmap are not
silently reclassified as implemented features.

## Checks and evidence

* Focused driver reproductions run against an isolated source copy with `-race`.
  Failures reproduce the five driver defect classes above; no race-detector
  warnings were emitted. Real-server probes use only connection-local temporary
  tables in the reserved Go schema and connection-local SQL mode changes.
* `go vet ./...` and all eleven existing Python recorder tests pass again.
* The actual live capture remains fresh. All immutable reference checksums and
  inverse identity/workload checks pass. Full benchmarks were not repeated and
  the live report was not replaced with review-only test totals.
* The previously recorded 958 PASS / 1 desired-gap FAIL / 7 SKIP remains the
  result of its original workload. It is not a claim that these additional
  reproductions passed. The new review evidence records its own nonzero Go exit.

Evidence: [review-only Go tests](../validation/results/review-20261112/review_test.go.txt),
[race-enabled reproduction log](../validation/results/review-20261112/go-review.log),
[recorder reproduction](../validation/results/review-20261112/recorder_probe.py),
[recorder log](../validation/results/review-20261112/recorder-review.log),
[CI skip log](../validation/results/review-20261112/ci-skip-review.log), and
[command/source manifest](../validation/results/review-20261112/capture.json).
The recorder reproduction exits zero because the probe itself ran successfully;
its log explicitly records the incorrect verifier acceptance and report defects.

Suggested correction order: R1/R2, then R3/R4/R5, then R6/R7/R8. Add regressions
before accepting those fixes; run the full documented suite and benchmark
comparison on the final corrected source. Nothing was committed, pushed or
published during this review.
