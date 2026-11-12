# First-stage review corrections

**Publication update, 12 November 2026:** Anton approved committing and pushing
this implementation and its retained validation evidence to the fork's `master`
branch. The receipts below describe the measured working tree before that
commit; their source hashes identify the delivered code. No benchmarks are
rerun for publication, and no package release is authorized. Earlier references
to uncommitted work describe the implementation/validation checkpoint.

12 November 2026. Anton authorized fixing every finding in the
[implementation review](STAGE1_CODE_REVIEW.md). This pass preserves the original
review evidence, workload bodies, original benchmark baseline and independent
first native-bulk reference. All work remains uncommitted.

| Finding | Correction | Persistent regression coverage |
| --- | --- | --- |
| R1: compressed bulk response association | Finish the compressed write/read transition before reading the bulk result | Bulk content, counters, subsequent commands, duplicate errors and cancellation over TCP, zlib, verified TLS, sockets and client certificates; an 8 KiB BLOB forces actual compression |
| R2: EOF drain panics | Read, drain and metadata cleanup use one bounded terminator decoder, including required warnings/status and checked non-NULL integers | Truncated terminators in both EOF modes and all three read/drain paths; metadata errors, valid multi-result status and terminator fuzzing |
| R3: cached metadata and legacy EOF | Consume the metadata EOF even when column definitions are omitted | Mock zero/one/multiple rows; real legacy negotiation and repeated Exec/Query alternation |
| R4: bulk zero dates | Preserve the prepared parameter zero-date convention before timezone conversion or truncation | UTC and positive/negative offset locations, zero/nonzero time, microseconds, truncation, invalid-year rejection before execute |
| R5: malformed rows and cleanup cancellation | Retire structurally invalid rows in both decoding paths; keep context cancellation active through draining and make Close idempotent on errors | Legacy/direct scanning, stalled peer cleanup with default timeouts, and valid application conversion errors that keep the same healthy session |
| R6: absent benchmark profiles | Publisher and verifier share checks for both reference profile sets, unique family/repetition identities, process metadata, finite required/custom metrics and recomputed medians | Removing a whole original/native profile, duplicate identities, invalid metrics, missing custom metrics, forged medians and actual verifier rejection |
| R7: missing CI feature execution | Add explicit Linux MariaDB 12.3 fixtures on Go 1.25/1.26/1.27; require TLS/X.509/socket/authentication and tagged protocol tests, race checks and all sixteen native benchmark smoke paths | Pure CI policy tests plus local Mac and isolated Linux execution of the runner and fixture provisioning |
| R8: report regeneration | Generate the stage overview, performance signal counts, native comparison table and precise workload units from recorded evidence; display missing rows explicitly | Regeneration preserves explanatory sections, distinguishes inherited units and 1/16/100/1000-row writes, and shows absent profiles |

An additional phase-fault regression found that cancellation during the prepare
write was converted into `driver.ErrBadConn`. Prepare now preserves the original
context error. Deterministic native tests cover prepare write/reply, execute
write/reply and statement-close write cancellation, confirming retirement in
every case. Real bulk rollback tests preserve duplicate-key identity and verify
that the caller's transaction can roll back completely.

## Safety, ownership and performance

The cancellation watcher remains active while cleanup performs network reads.
Structural protocol failures retire the physical connection and do not request
automatic replay of uncertain writes. A valid value that cannot be assigned to
the application's destination is a different case: its session remains reusable.
Checks added for row errors execute on error paths; the shared terminator decoder
runs at result boundaries rather than for every scalar value.

No implicit session reset, native fallback, batch splitting, transaction or
statement cache was added. Bulk keeps explicit pinned-connection ownership and
transient prepare/close. Those later performance/API choices remain on the
roadmap. The standard zero-time convention still obeys the server's SQL mode;
the driver does not promise that every strict mode accepts zero dates.

## CI scope and the existing session gap

The inherited OS/server matrix remains. The new fixture-backed feature job
explicitly targets Linux MariaDB 12.3 on Go 1.25, 1.26 and 1.27. Configuration
does not mean every hosted job has run. Local Linux verification uses a fresh
isolated 12.3.3 instance with real certificates and plugins; it does not change
the shared Homebrew server. Provisioning plugins requires the isolated-CI marker.

The original desired clean-pool assertion still fails because default checkout
is liveness-only. Its body, raw failing Go status and report history are retained.
CI allows only that exact assertion with its exact known-gap diagnostic, emits
a warning, and rejects additional failures, incomplete tests, missing mandatory
tests, skipped live MariaDB features and races. Package/build failures cannot
hide behind the known gap. Implementing an explicit reset-and-restore API remains
the next functional stage; the default contract was not silently changed.

The CI native benchmarks are single-iteration execution smoke checks. Their
timings never replace accepted seven-repetition Mac measurements. Workflow action
references remain unchanged; actionlint validates the edited workflow. Local
source and CI captures include driver/workload fingerprints and input-stability
checks, including the workflow and action lockfile in current workload inputs.

## Reproduction

Use the existing private local fixtures for the ordinary full capture:

```sh
python3 -m unittest discover -s validation -p 'test_*.py'
python3 validation/record.py --conditions 'Describe actual measurement conditions'
python3 validation/verify.py
```

`--correctness-only` retains a separate diagnostic capture. A missing benchmark
profile makes a full attempt incomplete and prevents it replacing the live
report. The report is now generated completely; no post-run numerical or caption
edits are required. [Live results](../validation/RESULTS.md) carry the measured
source fingerprint, complete workload results and all performance comparisons.

For fixture-backed correctness and benchmark smoke without publishing timing:

```sh
MARIADB_VALIDATION_CONFIG=/absolute/path/to/private/config.json python3 validation/ci.py
```

## Final validation receipt

The complete Mac capture is `20261112T082038200863Z`, fingerprint
`7e0376b88d5fce25d372680fece6d1faa3c4af6cf10bf8775616b4be79ea5b97`.
All 42 production/module inputs and all current workload/CI inputs match the
exact-source Linux receipt. Original/native frozen workload bodies and reference
checksums pass; no original passing assertion was lost.

| Check | Result |
| --- | --- |
| Mac ordinary correctness, unique leaves | **1,018 PASS / 1 known clean-session FAIL / 7 SKIP** |
| Mac race repetition, unique leaves | **940 PASS / same known FAIL / 7 SKIP; zero race warnings** |
| Linux arm64, Go 1.27.4, isolated MariaDB 12.3.3, unique leaves | **1,019 PASS / same known FAIL / 6 SKIP; zero race warnings**; one additional inherited case runs on Linux |
| Later Mac correctness-only confirmation, including tagged protocol tests under race | **1,018 PASS / same known FAIL / 7 SKIP; zero race warnings**; concurrency limited to two, no benchmarks |
| Static/workflow checks | vet and actionlint pass |
| Recorder/CI-policy regressions | all **24** pass |
| Length-prefix fuzzing | **222,582** executions; pass |
| Terminator fuzzing | **210,107** executions; pass |
| Full performance capture | all **200** profiles have **seven** distinct process repetitions; completeness issues: **0** |
| Report generation | regenerated report is byte-identical to the published local report |

**Counting clarification:** the frozen historical parser retained eleven passing
container test names whose children use multiple slash-separated name segments.
Its baseline-compatible totals in the full capture/live report are 1,029 on Mac,
951 for the ordinary race repetition and 1,030 on Linux. The table above excludes
every ancestor, including those eleven containers, and counts unique leaves.
All original passing test identities still pass; this is a reporting distinction,
not lost coverage. Raw logs and historical summaries remain preserved.

Full-run exit **1** remains intentional and visible because the unchanged
desired-default-clean-pool assertion is still an API gap. No newly found driver
defect or unexpected correctness failure remains in these captures. The CI
policy exits **0** while retaining that raw Go failure and its diagnostic.
Hosted GitHub Actions jobs and the Go 1.25/1.26 matrix have not been executed in
this local pass; the new workflow is configured and linted, not claimed as a
completed hosted matrix run.

**Timing acceptance remains open.** The full capture has 172 historical-profile
signals above +5% and 76 above +20%. Per-family observations show concurrent
Hearthstone, WindowServer and virtualization activity; there was no fresh idle
confirmation. These observations do not establish the cause of every slowdown.
The capture is preserved as measured, including every signal and repetition.

After those measurements completed, Anton reported heavy foreground use and
requested correctness checks only. No further benchmarks, including benchmark
smoke checks, run in this pass. The completed timing captures remain diagnostic
evidence, not performance acceptance; the original and native references remain
unchanged.

A separate, fixed-order diagnostic interleaves the unchanged original driver,
the pre-review stage and the corrected driver for four frozen 100-row integer
scan workloads, three repetitions at 500 ms. Corrected versus pre-review medians
range from **−11.1% to +0.4%**: no added >5% slowdown in those selected paths.
The unchanged binary also drifts from its historical timings. This narrow
control does not prove that all full-suite timing signals are environmental or
establish universal speedups; it never replaces the full capture or a reference.
An initial diagnostic filter expected a fifth top-level DSN case that the nested
filter did not execute; that stopped attempt and its four successful samples
are retained separately before the complete four-profile diagnostic.

Evidence: [final capture and analysis](../validation/results/review-fixes-final/analysis.json),
[complete raw capture](../validation/results/review-fixes-final/capture.json),
[Linux CI receipt](../validation/results/review-fixes-linux-final/capture.json),
[interleaved CPU controls](../validation/results/review-fixes-controls/capture.json)
and [live report](../validation/RESULTS.md). No timing-only confirmation was
substituted for the full-source capture. All changes remain uncommitted; no push
or package release occurred.

The later [correctness-only receipt](../validation/results/review-fixes-correctness-only/capture.json)
uses the same source fingerprint, retains the raw Go exit and every test event,
and independently verifies required features, absence of unexpected failures,
unchanged inputs and absence of race warnings. It includes all previously
passing identities and does not overwrite the earlier timing capture.
