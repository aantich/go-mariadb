# Stage 8 review corrections

All five findings in the [review](STAGE8_CODE_REVIEW.md) are addressed. Work
remains uncommitted on `master`; readiness remains **Alpha**. Benchmarks,
benchmark smoke, profiling and publication remain deferred.

## GORM behavior and framework coverage

The adapter embeds the concrete shared SQL dialect, preserving GORM's optional
configuration and savepoint methods. Compile-time assertions protect the method
set. Unit cases verify default timestamp rounding and a caller's custom clock.
Live cases verify automatic create/update timestamps after reload, explicit
savepoints, an inner rollback while the outer transaction continues, and an
inner success committed with its outer transaction. They run with plain, zlib,
verified TLS and TLS + zlib connections.

Expiry tests now warm and reuse a standard SQL statement, a sqlx named statement
and GORM's actual prepared cache. The GORM test also checks that its cache retains
the same prepared handle. Every case requires a different physical connection
and a valid result after lifetime expiry. No scheduler sleeps were added.

Temporary source-copy controls prove that removing client construction fails all
three expiry leaves. Restoring the old narrow-interface GORM wrapper fails all
eight new live dialect cases. The unchanged control passes its three expiry
leaves. The original review's unmodified standalone GORM probe now finds both
optional interfaces, invokes the nested callback and roundtrips timestamps.
See [control outcomes](../validation/results/stage8-review-fixes-20261114/regression-controls.json)
and [fixed probe](../validation/results/stage8-review-fixes-20261114/gorm-probe-fixed.log).

## Native evidence and report reliability

New native Linux application receipts use `mariadb-applications-native-v2`.
They discover and snapshot the newest matching predecessor, verify its hashes,
commands, inventory and raw events, and reject disappearing passing leaves.
The publisher independently validates that history and rejects omission of a
newer eligible predecessor. Historical v1 receipts remain unchanged and usable
as predecessors under their original frozen command/inventory/dependency floor;
current publication requires v2.

Profile identity includes compiler, host, server settings, environment and
meaningful fixture options. It excludes random socket paths and source/fixture
hashes so test growth or fixture edits cannot silently erase history. Provenance
and source hashes remain required separately. The final native run links the
previous accepted Stage 8 Linux receipt.

Native stages stream output to disk. Timeouts retain partial output and an
explicit timeout/exit status. Preflight and compiler errors retain sanitized
phase/type diagnostics. Server-log or receipt-write failures cannot bypass the
fixture parent's process-group/server cleanup. Arbitrary exception messages and
subprocess environments are not serialized into failure metadata.

The application cohort reconstructs its OS/architecture scope from verified
compiler identities. Tests cover Mac, Linux and permitted mixed-family clients,
unknown native contracts, lost extra Linux-only passes, altered predecessors,
omitted history, timeouts with buffered or streamed partial output, and setup/
server-diagnostic failures. The immutable v1 application contract is unchanged.

## Verification

See [applications](../validation/APPLICATIONS.md),
[core correctness](../validation/RESULTS.md), the
[twelve-cell core matrix](../validation/COMPATIBILITY.md) and the
[confirmation manifest](../validation/results/stage8-review-fixes-confirmation.json).
Source fingerprint: `c2468c56caf4c77b64c30f1e3458759f45167759e9f2953cc8cc2feb9bd7608b`
(71 driver/module/native and 130 workload/harness/CI inputs).

Full current-version correctness passes **1,637 Mac / 1,640 Linux** unique Go
leaves with **zero failures**, the existing **8/5 skips**, race/vet and **128
harness tests** on each platform. All eight versioned application profiles and native Linux pass **41 leaves** each,
ten more than the original Stage 8 inventory, with no failures or skips. The
core matrix has **1,219 passes / 6 skips** for Go 1.25/1.26 and **1,303 passes /
6 skips** for Go 1.27 in every server family. Required integration cases cannot
skip. Counts exclude parent test labels and do not mix application/core suites.
`python3 validation/verify.py` passes. Independent controls require it to reject
a missing/incomplete application cohort, corrupt native predecessor, historical
v1 receipt presented as current, or fabricated platform summary.

The original 68 core runtime/native inputs and core module dependencies remain
unchanged. The implementation edit affects the optional GORM adapter; the other
changes strengthen tests, evidence handling and documentation. Historical
benchmark/reference bytes remain unchanged, so this pass makes no speed claim.

All 620 sealed initial Stage 8 artifact byte sequences and all 12 sealed review
artifact byte sequences remain available, with archived copies of changed live
files. Earlier failed development checks remain retained. The initial review's
pending statements describe that historical source and are superseded here.
Automated applications do not establish production use, cluster/proxy failover,
Windows or other framework-version compatibility. The original Stage 8 release
gates remain open. No new branch, commit, push, tag or release was created.
