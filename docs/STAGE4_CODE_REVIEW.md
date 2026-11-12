# Stage 4 review: four findings (corrected)

**Follow-up:** All four corrections are implemented and validated. See the
[fix record and new evidence](STAGE4_REVIEW_FIXES.md) for current status; the
findings and counterexamples below preserve the original review.

12 November 2026. Reviewed the complete uncommitted Stage 4 driver, tests,
benchmark workloads, collection/verification changes, documentation and retained
evidence against parent `187fd7d`. Executable inputs remain
`4d2c8a294bd0495610075aced928a321daf12aab7e44cd048703a04e618e147c`.
This review adds reports and counterexamples only; **no production or harness
fixes, benchmarks, profiling workloads, commit or push** were performed.

| Priority | Finding | Evidence |
| --- | --- | --- |
| P1 | Closing an old native handle can corrupt the next pool borrower's active Rows | Reproduced against real MariaDB with race detection |
| P2 | New passing subtests can disappear without triggering pass-preservation checks | Actual correctness publisher accepted a reconciled synthetic omission |
| P2 | Unit-bulk benchmark skips its result checks when a helper returns nil without error | Source inspection of the new benchmark branch |
| P3 | Diagnostic failures can discard the capture/provenance record | Mocked build timeout and failed final server probe |

## P1: close after connection return is not safe during another borrower's Rows

[`NativeStmt.Close`](../native_statement.go#L180) delegates directly to
`sql.Stmt.Close`. The driver statement's [cleanup](../statement.go#L55) sends
COM_STMT_CLOSE on its physical connection. The database/sql connection lock
serializes individual driver calls, but does not hold that lock for the entire
lifetime of an open result stream.

With the default liveness-only pool policy, this valid documented sequence fails:

1. Prepare a native handle on a pinned connection.
2. Return the connection without closing the handle.
3. Borrow the same physical connection and begin consuming a 1,000-row query.
4. Close the old native handle while the new borrower still has active Rows.

The close path resets the packet sequence before discovering the busy read
buffer. Close returns `busy buffer`; the next row expects packet sequence zero
but receives five. The query fails with `commands out of sync` after **1/1,000
rows**, and the physical connection is retired. The public guide/API explicitly
allow cleanup after connection return, so the old handle must not interfere with
the next owner. Managed-reset checkout avoids this particular close command by
invalidating the old generation, which explains why existing reset tests pass.

The [ordinary Go correctness probe](../validation/results/stage4-review-20261112/close-after-return_test.go.txt)
uses plain TCP and a sequence-table SELECT; it does not execute a benchmark.
Its [race-enabled failure log](../validation/results/stage4-review-20261112/close-after-return.log)
retains the actual errors. No application schema or shared-server global was changed.

**Fix:** arrange generation-checked cleanup at a safe command boundary, including
after return to the pool. A lock around one Close call is insufficient. Checking
only buffered bytes is also insufficient: active Rows can have an empty read
buffer or expose borrowed data. Add regressions for the next borrower's active
text/prepared/cursor Rows, borrowed values, compression and reset generations.

## P2: preserve the latest correctness passes independently of timings

The Stage 4 [required-test list](../validation/ci.py#L46) requires parent test
names. The [correctness publisher](../validation/correctness.py#L52) protects
passing identities from `results/current.json`, which is intentionally the older
200-profile timing capture. It does not compare against the newer
`correctness-current.json`. The full recorder likewise saves the prior timing
capture as its [historical reference](../validation/record.py#L308).

That older capture predates both the Stage 3 review regressions and Stage 4's new
passing leaves. Removing one child while its parent and siblings pass therefore
does not trigger a missing-test error. The synthetic
[counterexample](../validation/results/stage4-review-20261112/pass-protection-probe.json)
removes `TestNativeStatementLifetime/stale_close_does_not_close_reused_id`
consistently from copied raw events and their receipt, then recalculates the log
checksum. The real `correctness.summarize` accepts **1,259** passes and reports
`prior_passes_lost: []` despite the published correctness receipt having 1,260.
No source, original receipt, baseline or live pointer was changed by that probe.

This is an existing policy gap left open by the new stage, not evidence that an
earlier test actually disappeared. The Stage 4 confirmation independently checked
all previous pass identities and remains valid for its recorded runs.

**Fix:** preserve and checksum the preceding matching-profile correctness receipt
as an independent historical reference, for both correctness-only and full runs.
Require every prior passing leaf to remain passing. Do not require a new benchmark
measurement before protecting new correctness cases, and do not conflate Mac/Linux
fixture-specific skips. Add an omission regression to the actual publication path.

## P2: require unit results based on the selected benchmark method

The [new bulk benchmark](../validation/stage4_bench_test.go#L101) checks unit
length/counters only inside `if units != nil`. If `units_once` or `units_reused`
inserts the data but returns `nil, nil`, the benchmark skips every unit-result
assertion and its count/sum query still passes. A broken implementation could
therefore produce an apparently valid result for one of the new batch-size
profiles. Existing small correctness cases do not make the benchmark's own
contract complete for every measured alternative.

**Fix:** require the expected unit count whenever a unit-results method is
selected, including when the slice is nil, then validate each counter. Keep the
same checks in timed alternatives. This finding is static; no benchmark function
was invoked to demonstrate it.

## P3: retain failed diagnostic attempts

[profile_reads.py](../validation/profile_reads.py#L80) writes its capture only
after all subprocesses and the final server probe have returned normally.
`TimeoutExpired`, `OSError` and provenance errors bypass that write. A failed
final probe can leave CPU/heap files without the source/conditions/checksum
receipt that makes them attributable to a particular attempt. Retrying also
requires a new output directory, because the partial directory already exists.

The [mocked failure proof](../validation/results/stage4-review-20261112/profile-failure-probe.json)
shows a build timeout and an unsuccessful final probe both leaving no capture;
the latter retains synthetic CPU/heap artifacts. Every subprocess/server probe
was mocked. Those artifacts were temporary placeholders, never measurements.

**Fix:** write an initial receipt and finalize a failed, redacted capture on
exceptions, preserving available partial output and checksums. Record unavailable
provenance explicitly, return failure and leave all live/reference pointers alone.

## Checks and performance limits

The existing focused native-statement tests pass with race detection, bounded
concurrency and no benchmark flags. All **62 harness tests** pass;
`validation/verify.py` still verifies the original Stage 4 correctness receipt,
immutable references and workload migrations. The new live close-after-return
probe fails as recorded above, exposing a missing case in that earlier coverage.
No complete suite rerun was needed for a report-only review.

No obvious new copying/decoding work was added to ordinary row scanning; the
packet/row/scanner sources remain unchanged. Measured throughput/allocation
acceptance is still pending. For the later read investigation, collect CPU and
full-allocation profiles in separate diagnostic runs where practical, so the
every-allocation sampler does not contaminate CPU attribution. This is a
measurement recommendation, not an additional demonstrated driver defect.

The [review receipt](../validation/results/stage4-review-20261112/capture.json)
links the probes and checksums. Production inputs and the existing 200-profile
timing pointer remained unchanged. All four findings were pending at this review;
the follow-up linked above records their corrections and newer validation.
