# Stage 7 review corrections

Anton authorized fixing all three [review findings](STAGE7_CODE_REVIEW.md) on
13 November 2026. Work continues directly on `master`, parent
`2a5fe58e5b15f52db7a375e301c2c09fd6e20e34`. New corrections remain uncommitted.
Readiness remains **Alpha**. No benchmarks, smoke benchmarks or profiling ran.

## History protection survives suite growth

The original v1 commands and minimum required inventory are frozen in
`validation/compatibility-contract-v1.json`, protected by its own SHA256 and
included in the executable-input fingerprint. Historical receipts use that
versioned contract; new captures must also satisfy today's full required
inventory. Adding mandatory cases no longer invalidates qualified earlier
receipts or erases their passing leaves. Command changes require a new contract
version rather than rewriting the meaning of old evidence.

Discovery examines contract-matching captures throughout `validation/results`,
independently of stage-directory names. The collector now requires outputs within
that permitted tree. Failed attempts and early prototypes without the verifiable
inventory remain preserved but cannot establish prior passes. A qualified
successful predecessor that becomes corrupt fails validation instead of silently
resetting history. Paths are resolved consistently on macOS and Linux.

## Current compatibility claims are independently verified

`validation/compatibility_cohort.py` is the maintained validator and publisher for
the complete twelve-cell cohort. It reconstructs compiler/server identities,
counts, source fingerprints, restart/cleanup facts and artifact hashes from
validated raw receipts. Missing, duplicate, relabelled, stale or altered cells
are rejected. Evidence paths and log symlinks cannot escape the results tree.
The public table and machine-readable index come from the same verified data.

The standard `python3 validation/verify.py` now requires that cohort as well as
fresh full correctness and immutable references. Running only the current-server
suite cannot make stale older-server compatibility evidence look current.
The cohort publisher consumes saved receipts; it starts no server and executes
no Go tests or benchmarks. Reproduction commands are in the
[validation guide](../validation/README.md).

## Failed setup retains useful, safe diagnostics

Failures retain a phase and exception type in `failure.log` and the capture.
Docker failures additionally preserve their safe operation, exit/timeout status
and bounded, password-redacted output. Compiler/client preflight failures also
leave receipts. Restart/controller errors retain their sanitized diagnostics in
the stage log; cleanup and server-log failures have separate diagnostic records.

Private subprocess environments and arbitrary exception messages are not
serialized. Diagnostic writes are best effort: a disk or permission error cannot
prevent cleanup of owned resources. Tests cover startup failures, Docker timeout
and execution errors, preflight failures, secrecy and failed diagnostic writes.

## Correctness and evidence

Final executable input fingerprint:
`a16c00cf01a35b193ab251edbadb1a04ece062906b8a3f12bffbabd6cee449f1`
(68 driver/native files, 112 test/harness/CI inputs). All 68 driver/native input
hashes and all Go test inputs are unchanged from the reviewed implementation;
these corrections affect validation, diagnostics and documentation.

| Profile | PASS | FAIL | SKIP | Race / vet / harness |
| --- | ---: | ---: | ---: | --- |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,637 | 0 | 8 | PASS / PASS / 113 PASS |
| Isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,640 | 0 | 5 | PASS / PASS / 113 PASS |

No earlier passing Go leaf was lost. Harness coverage increases from 99 to 113.
The separate [compatibility report](../validation/COMPATIBILITY.md) records all
twelve final-source Go 1.25.14/1.26.11/1.27.4 × MariaDB
10.11.19/11.4.13/11.8.9/12.3.3 profiles, with zero failures and unchanged
1,219/1,303 passing leaves per older/newer compiler profile. Its scope remains
macOS arm64 clients and Linux arm64 Docker servers; full current-version Linux
coverage is independently executed. No new remote GitHub Actions run is claimed.

Restoring the reviewed history-floor, directory-prefix and lost-diagnostic
implementations makes the intended new regressions fail. A separate control
presents missing, stale and incomplete compatibility pointers to the real
standard verifier: corrected code rejects them; restoring the old verifier
incorrectly accepts them. All controls run against temporary/in-memory changes
without editing the checked source or actual evidence pointers.

The first matrix attempt exposed an unqualified early development capture;
the history check was corrected and that attempt retained. Full runs whose source
changed during collection are also retained as ineligible. Only complete,
successful receipts with the final fingerprint supply the live reports. Three
early matrix cells were refreshed; nine already matching cells were retained
after validation. Original Stage 7/review evidence and all 360 original sealed
artifacts remain recoverable byte-for-byte from parent commit `2a5fe58`.
Original/reference timings and the historical timing pointer are unchanged.

Evidence:

* [Current correctness](../validation/RESULTS.md) and [compatibility](../validation/COMPATIBILITY.md)
* [Mac receipt](../validation/results/stage7-review-fixes-mac-final-2/capture.json)
* [Linux receipt](../validation/results/stage7-review-fixes-linux-final-2/capture.json)
* [Checks, failures and controls](../validation/results/stage7-review-fixes-20261113)
* [Final confirmation and hashes](../validation/results/stage7-review-fixes-confirmation.json)

Application pilots, broader operating-system/architecture deployments,
production failover topologies and the existing explicit CDC scope limits remain
validation work. This review adds no driver API, production certification or
measured performance claim. No new commit, push or release was made.
