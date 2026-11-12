# Stage 4 review corrections

12 November 2026. Anton authorized fixing all four findings in the
[Stage 4 review](STAGE4_CODE_REVIEW.md). Anton subsequently authorized publication
of the complete stage and corrections to `master`. The saved receipts preserve
their original prepublication context. No benchmarks or profiling workloads ran; the new
62 profiles remain unmeasured and the older timing/reference bytes are preserved.

## Statement cleanup respects the complete Rows lifetime

The connection tracks its active Rows from successful query construction to
Close. Statement Close releases local ownership immediately but queues its server
ID if another result stream is active. Rows drains/resets its response, then
sends queued closes while its cancellation watcher is still active. Every queued
ID includes its session generation; reset/retirement cannot close a recycled ID.
A deferred transport failure retires the connection and appears in Rows.Close/Err.

This applies to ordinary driver statement cleanup as well as `NativeStmt.Close`.
It handles the documented lifetime in which a native handle is closed after its
sql.Conn returns to the pool and the next borrower has started reading. Keeping
ownership until Close also protects borrowed bytes when the read buffer is empty
and cursors between batches. A completed older decoder cannot release a newer
result owner's queued cleanup. Explicit reset/native commands reject active Rows
before touching the protocol state.

Tracking and cleanup run at query/Close boundaries. No row decoder/scanner loop
was changed. Queue allocation occurs only when statement cleanup overlaps Rows;
ordinary idle statement cleanup still sends its close immediately. Its performance
impact is unmeasured.

Persistent live regressions cover seven transport/metadata modes, text/prepared/
cursor queries, full consumption, early close, three concurrent old-handle closes,
borrowed values and physical connection reuse. Fault tests cover buffered and
empty-buffer ownership, wire EOF, owner transfer, stale generations, retirement,
write failure and cancellation during a stalled queued close. The earlier review
counterexample now succeeds; restoring unsafe close through an overlay makes the
new regression fail again after 1/1,000 rows.

## New correctness passes are protected before new timings exist

Each correctness/full attempt preserves `prior-correctness.json` with its own
checksum and a `correctness-history-v1` contract. Collection selects the newest
successful matching compiler/host/server-version/instrumentation profile from
saved correctness/full receipts. Its raw test events, command contract, source
fingerprint, log checksums and server snapshots must corroborate the reference.
Historical code need not be fresh: the prior passing leaves are the comparison.
New profiles explicitly record that they have no earlier matching reference.

The collector and publisher/verifier require every prior passing leaf to remain
passing. A missing or modified snapshot, omitted/skipped leaf, failed audit or
profile mismatch cannot publish a successful result. Mac/Linux fixture-specific
passes remain separate. The snapshot is retained with the new run, so advancing
the live pointer cannot erase its reference. Prior timing-pass protection and
immutable original references remain independent checks.

Policy tests exercise the actual correctness publisher, full collector and full
verifier. They reject an omitted newer child even when its parent passes and the
older timing capture never contained that child. Historical receipts keep their
original schema and bytes; this policy is not claimed retroactively.

## Unit benchmarks reject missing results

The selected `units_once`/`units_reused` method now always requires the expected
unit count and counters. A nil result cannot bypass validation after successful
insertion. A shared validation helper permits ordinary correctness tests for nil,
short and incorrect results without invoking a benchmark. An overlay that
restores nil acceptance makes that regression fail. This strengthens the new,
still-unmeasured workload before its first reference is frozen.

## Diagnostic failures keep their evidence

The profiling tool writes an atomic initial receipt and checkpoints each stage.
Timeouts, OS errors and before/after server-probe failures retain a failed capture
with source/conditions, failure location/class, available artifact checksums and
redacted partial subprocess output. Interrupted attempts remain explicitly
incomplete. None can publish timing pointers or references.

Mocked regression cases cover both probe failures, build/profile timeout and
build OS errors, including credential-shaped text in partial output. Every
subprocess/probe in those tests is mocked; no profiling workload executes.
The default tool invocation remains dry-run.

## Verification and evidence

Final executable-input fingerprint:
`d48b450072dd1b8e7405efdaa5888e01c18849b6e40d8ec927fbb62e41603fc7`
(53 production/module files and 81 workload/CI inputs).

| Final profile | PASS | FAIL | SKIP | Initial Stage 4 passes preserved | Added passes |
| --- | ---: | ---: | ---: | ---: | ---: |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,304 | 0 | 8 | 1,260 | 44 |
| Isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,307 | 0 | 5 | 1,263 | 44 |

Both final matching-source runs pass race detection, vet and **69 harness tests**.
The new history guard automatically protects 1,304 prior Mac leaves and 1,263
prior Linux leaves. The final Linux run also preserves every one of the 1,307
Go passes from its first attempt. That earlier attempt passed Go/race/vet but
failed one synthetic harness fixture because it inherited the isolated server's
instrumentation flag. The fixture now explicitly selects its matching synthetic
profile; both platforms were reconfirmed. Those earlier receipts remain intact.

The two defect-restoration controls fail for the intended reasons: unsafe close
again corrupts the next borrower's stream after 1/1,000 rows; nil unit acceptance
again bypasses required results. They used the identical production/Go workload
inputs, before the final Python-only fixture correction. No benchmark function
was executed. Formatting, diff checks, actionlint, source freshness, snapshot
checksums and immutable-reference verification pass. The two-CPU/4 GiB Linux
fixture was removed; shared-server globals were unchanged.

The [confirmation](../validation/results/stage4-review-fixes-confirmation.json)
links final Mac/Linux receipts, preserved pass counts, negative controls and
checksums. The [live report](../validation/RESULTS.md) uses current correctness
separately from historical timing samples. The original review's counterexamples
and Stage 4's earlier receipts remain preserved.

All changes remain uncommitted. First measurements of the new profiles, read-path
attribution and measured performance acceptance remain later work. No commit,
push or package release occurred in this correction pass.
