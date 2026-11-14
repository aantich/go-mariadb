# Current authorization, 14 November 2026

Anton approved Stage 8: real application integration and Alpha release
preparation. Add pinned GORM/sqlx workflows, lifecycle/restart recovery, an
optional dedicated GORM adapter, independent core consumer checks, migration
examples and a maintained application evidence gate alongside the core matrix.
See `docs/STAGE8_APPLICATIONS.md`, `docs/APPLICATIONS.md` and
`validation/APPLICATIONS.md`. Preserve all Stage 7 review corrections and earlier
evidence. Keep all work uncommitted on existing `master`; no branch, tag,
commit, push or package publication is authorized. Do not run benchmarks,
benchmark smoke or profiles. Readiness remains Alpha; automated application
workflows do not establish production use. The core module's dependencies
remain unchanged; framework dependencies live in nested modules. Both the
core and optional module still use development replacements until an approved
public version is selected. Comments must explain public ownership, error and
configuration contracts. Historical status below describes its capture time.

Stage 8 final source is `9e8bdde9…ab85de` (71 driver/module/native and 129
test/harness/CI inputs). The original 68 runtime/native inputs and core module
dependencies remain byte-identical. Core Mac/Linux correctness passes
1,637/1,640 leaves, zero failures, unchanged 8/5 skips, race/vet and all 122
harness tests. Twelve core matrix cells pass at this same source. Eight
Go 1.25/1.27 application cells plus native Linux each pass 31 leaves with no
failures/skips. The live correctness/core/application reports have independent
cohorts; `verify.py` requires all of them. See
`validation/results/stage8-confirmation.json` for final evidence and controls.
All 192 Stage 7 review-fix artifact byte sequences remain available; changed
live documents have archived copies under `results/stage8-20261114/previous-artifacts`.
Historical timing/reference bytes stay unchanged. Source-changing, setup and
archive-metadata attempts remain saved and excluded from current acceptance.

# Previous authorization, 13 November 2026

Anton requested fixing all three Stage 7 review findings. The checkout already
contained Stage 7 and its review in `2a5fe58e5b15f52db7a375e301c2c09fd6e20e34`;
new corrections are complete and uncommitted on `master`. See
`docs/STAGE7_REVIEW_FIXES.md` and
`validation/results/stage7-review-fixes-confirmation.json`. Source
`a16c00cf…e449f1` has unchanged driver/Go-test inputs, 1,637 Mac / 1,640 Linux
passing Go leaves, zero failures, unchanged 8/5 skips and 113 passing harness
tests per platform. All twelve Go/server matrix cells pass at that same source.
Historical contracts/inventories and arbitrary output names preserve earlier
passes; the standard verifier now requires a complete corroborated compatibility
cohort, and setup failures retain sanitized diagnostics. Restored defects fail
the intended controls. Failed/source-changing attempts and original evidence are
preserved; original sealed bytes remain in parent `2a5fe58`. Live correctness
and compatibility reports are refreshed; timing/reference bytes are unchanged.
No benchmarks, smoke runs or profiling occurred. Keep corrections uncommitted
on `master`; no new commit, push, release or branch is authorized. No new remote
CI execution is claimed. Earlier pending/uncommitted statements below describe
their capture time and are superseded by this status.

# Previous authorization, 12 November 2026

Stage 7 is complete and uncommitted on `master`, parent `ec42783d`. Final source
`98ba80ed…526635f` passes all 12 MariaDB 10.11.19/11.4.13/11.8.9/12.3.3 ×
Go 1.25.14/1.26.11/1.27.4 correctness profiles. Full current-version checks pass
1,637 Mac / 1,640 Linux leaves, zero failures, unchanged 8/5 skips, race/vet and
all 99 harness tests. All 1,588/1,591 Stage 6 passes survive, with 49 added leaves
per profile. See `docs/STAGE7_COMPATIBILITY.md`, `validation/COMPATIBILITY.md`
and `validation/results/stage7-confirmation.json`. Empty GTID-list padding,
Darwin zombie reaping, stable restart endpoints and owned-only diagnostics are
corrected with regressions. Failed and stale-source attempts remain preserved.
No benchmarks, smoke runs or profiling occurred; references/timing bytes stay
unchanged. Stage 7 is not committed, pushed or released. Earlier publication
authorization applies to Stage 6; wait for a new instruction before publishing
this stage. Preserve source/evidence and the inherited branch.

Stage 6 publication is complete in user-authorized commit `ec42783deb9727a529f3f9c29a80e7ed4d585e7a`,
pushed to the fork's `master`. Prior uncommitted statements describe capture time.
Anton then authorized Stage 7 and selected version compatibility and failure
recovery: MariaDB 10.11/11.4/11.8/12.3, capability-aware API assertions and
deterministic disconnect/transaction recovery. See `docs/STAGE7_COMPATIBILITY.md`.
Implement directly on `master`; leave the new stage uncommitted for review.
Correctness/race/static checks only: no benchmarks, smoke benchmarks or profiling.
Preserve all earlier receipts, reference bytes, frozen workloads and pass history.
No release or upstream submissions are authorized.

Stage 6 review corrections are complete under Anton's subsequent “fix all”
instruction. `docs/STAGE6_REVIEW_FIXES.md` and
`validation/results/stage6-review-fixes-confirmation.json` record corrected
source `69f760d5…03c043`: 1,588 Mac / 1,591 Linux passing leaves, zero failures,
8/5 skips, race/vet/89 harness tests passing, and 84 added passes per profile.
The seven findings below are historical, now resolved. Keep all changes
uncommitted on `master`. No benchmarks, smoke benchmarks or profiling workloads
ran; do not run them or commit/push without a new instruction. Original review,
receipts and timing/baseline bytes are preserved; changed public documents have
hash-matched historical copies under the review-fix evidence directory.

Stage 6 review, later on 12 November: Anton requested a review pass. See
`docs/STAGE6_CODE_REVIEW.md` and `validation/results/stage6-review-20261112`.
Six correctness/validation findings remain pending: CI initializer resolution,
cursor-enabled Config reuse, backward file checkpoints, malformed EOF acceptance,
orphaned fixture descendants and unreachable GTID/row payload fuzz paths. A
seventh finding is redundant full-payload allocation/copy during expansion.
Live/overlay/Python probes reproduce the new cases; existing focused race and
82 harness checks still pass. Source `f1ae6b97…2e5370b` and all 100 Stage 6
artifacts remain unchanged. No implementation/permanent-test fixes, full-suite
rerun, benchmark/smoke/profile execution, commit or push occurred. Preserve all
counterexamples and prior receipts; fixes need a subsequent instruction.

Stage 6 authorization, later on 12 November: Anton requested implementing the
complete optional MariaDB CDC/binlog stage. Use a dedicated replication connection,
bounded event/table/row decoding, complete-group checkpoints, consumer-controlled
acknowledgements, GTID resume, isolated live/fault tests and public documentation.
Work directly on existing `master`, preserve earlier changes/evidence/references,
and leave this stage uncommitted. Correctness/race/static checks only: no benchmark,
smoke or profiling workloads. No commit, push or package release is authorized.

Stage 6 implementation is complete and uncommitted. `docs/STAGE6_CDC.md` and
`validation/results/stage6-confirmation.json` record final source `f1ae6b97…2e5370b`:
1,504 Mac / 1,507 Linux PASS, zero failures, 8/5 skips; all 1,398/1,401 Stage 5
passes preserved with 106 new leaves per profile. Race/vet/82 harness checks,
two correctness fuzz campaigns, five defect-restoration controls, CGO-disabled
checks and Windows/386 cross-compilation pass. CDC is a separate Alpha package;
XA/two-phase ALTER/engine-specific replication and production topology pilots
remain explicit scope limits. Live correctness is updated; timing/reference
bytes are unchanged. Do not rerun benchmarks or commit/push without instruction.

Stage 5 publication authorization, later on 12 November: Anton requested commit
and push of the full current Go driver work to the fork's existing `master`,
including Stage 5, its three review corrections, measurement-preparation docs
and saved evidence. This supersedes earlier uncommitted-only instructions for
this delivery. Verify the completed source/artifact receipts; do not repeat
tests or benchmarks for publication. No package release is authorized. The
benchmark/profiling restriction remains in force. Historical receipt statements
describe their original capture context. The next functional-stage proposal is
optional transaction-aware MariaDB CDC/GTID streaming; implementation requires
a later instruction to proceed.

Stage 5 review corrections completed: `docs/STAGE5_REVIEW_FIXES.md` and
`validation/results/stage5-review-fixes-confirmation.json` record all three fixes.
Final source `9d7c4b8c…29da7807` has 1,398 Mac / 1,401 Linux PASS, zero failures,
8/5 skips, all 1,377/1,380 earlier passes preserved and 21 new leaves per profile.
Race/vet/77 harness checks pass on both platforms. Three valid defect-restoration
controls fail the intended regressions; CGO-disabled checks and Windows fallback
cross-compilation pass. Live correctness is updated, timing/reference bytes are
unchanged and owned containers/caches are removed. No benchmarks, smoke/profile
workloads, commit, push or release occurred. Keep all changes uncommitted.

Stage 5 correction authorization, later on 12 November: Anton requested fixing
all three review findings. Implement permanent regressions, confirm correctness,
race/static/harness checks on Mac and the disposable Linux profile, preserve
all historical receipts/timings and update the live correctness report. Work
on existing `master` and leave changes uncommitted. No benchmark, smoke or
profiling workload, commit, push or release is authorized.

Stage 5 review, later on 12 November: Anton requested a review pass. See
`docs/STAGE5_CODE_REVIEW.md` and `validation/results/stage5-review-20261112`.
Three P2 findings are pending: external-auth error secrecy after plugin switches,
unbounded native work after caller cancellation despite SQL pool limits, and
acceptance of missing external-auth provenance in current receipts. Two Go
overlay probes fail their new assertions under race detection; the real current
correctness validator accepts the provenance omission. Existing focused auth
tests, 75 harness tests and freshness checks pass. No production/permanent-test
fixes or benchmark/profile workloads were made. Preserve the earlier complete
green receipts as recorded coverage, not coverage of these new counterexamples.
Keep all work uncommitted on `master`; no commit, push or release is authorized.

Stage 5 authorization, later on 12 November: Anton requested implementing the
next functional stage and deferring benchmarks. Implement opt-in interactive
PAM and principal-pinned GSSAPI with isolated real fixtures, public documentation
and correctness/race/static checks only. Preserve the Stage 4 preparation changes,
all historical timing samples and references. Work on existing `master`; leave
changes uncommitted. Do not run benchmark, smoke or profiling workloads.

Stage 5 final receipt: `docs/STAGE5_AUTHENTICATION.md` and
`validation/results/stage5-confirmation.json`. Fingerprint `c65a2f7f…e652171a`
matches 60 production/module/native and 93 workload/CI/fixture inputs on both
platforms: 1,377 Mac / 1,380 Linux PASS, zero failures, 8/5 named skips, race/vet
and 75 harness checks passing. All 1,304/1,307 Stage 4 passes are preserved,
with 73 added leaves per profile. Real PAM/Kerberos, native context cleanup,
permission/downgrade guards and failed-receipt retention are tested. Two valid
defect-restoration overlays fail their security regressions. CGO-disabled tests
and Windows fallback cross-compilation pass; Windows SSPI/NTLM is not implemented.
Earlier fixture/mock failures remain retained. No benchmarks/profiling ran;
timing pointers/references remain unchanged. Keep everything uncommitted.

Measurement preparation, later on 12 November: Anton requested proceeding with
the next stage, then explicitly selected "Continue preparation only; no benchmarks
yet". Prepare the runbook and read-only readiness evidence only. Do not execute
benchmark functions, one-iteration smoke runs or CPU/allocation profiling. No new
commit, push or release is requested. Preserve all live results and references.
See `docs/STAGE4_MEASUREMENT_PLAN.md` before the later authorized measurement.

Stage 4 publication authorization, later on 12 November: Anton requested commit
and push of the complete Stage 4 implementation, all four review corrections,
documentation and saved evidence to the fork's existing `master`. This supersedes
the earlier uncommitted-only instructions for this delivery. Verify the existing
source receipt; do not repeat tests or benchmarks just for publication. The
benchmark/profiling restriction remains in force. No package release is authorized.
Earlier uncommitted status in receipts describes their original capture context.

Stage 4 correction authorization, later on 12 November: Anton requested fixing
all four findings. `docs/STAGE4_REVIEW_FIXES.md` records safe deferred cleanup,
independent correctness history, mandatory unit validation and failed diagnostic
receipts. Keep all changes uncommitted on `master`. Correctness/race/static checks
and focused regression controls only; no benchmark or profiling workload execution.

Completed correction receipt: `validation/results/stage4-review-fixes-confirmation.json`.
Final source `d48b4500…1603fc7` has 1,304 Mac / 1,307 Linux PASS, zero failures,
44 added passes per profile and no lost earlier passes. Race/vet/69 harness checks
and static checks pass. Restoring unsafe close/nil-unit acceptance makes the new
regressions fail. An initial Linux synthetic-fixture flag mismatch is corrected
and preserved as an earlier attempt; both final profiles are green. No benchmarks
or profiling workloads ran. Old timings/references are unchanged, all four
findings below are resolved, and the stage remains uncommitted on `master`.

Stage 4 review, later on 12 November: `docs/STAGE4_CODE_REVIEW.md` records four
pending findings: unsafe old-handle close during the next borrower's active Rows
(live protocol corruption), missing newer correctness-leaf protection, nil unit
results escaping benchmark assertions, and lost diagnostic receipts on exceptions.
Evidence is in `validation/results/stage4-review-20261112`. Existing native race
tests and 62 harness checks still pass; the new live cleanup probe fails. No
production/harness fixes, benchmarks, profiling workloads, commit or push were
performed. The earlier green Stage 4 receipt remains historical recorded coverage;
it does not cover the newly reproduced cleanup case. Keep changes uncommitted.

Stage 4 authorization, later on 12 November: Anton approved the performance and
statement-reuse stage but is heavily using the Mac. Implement reusable native
statements, benchmark workloads/tooling and correctness coverage only. Do not
execute any benchmark function, including one-iteration smoke or profiling runs.
Do not invent timings or freeze unmeasured references. Keep work uncommitted on
`master`. Use bounded correctness/race/static checks and preserve prior captures.

Completed Stage 4 receipt: `docs/STAGE4_STATEMENT_REUSE.md` and
`validation/results/stage4-confirmation.json`. Final source `4d2c8a29…18e147c`
has 1,260 Mac / 1,263 Linux passing leaves, zero failures and 38 new passes per
profile, retaining all 1,222/1,225 earlier passes. Race/vet/62 harness checks and
static checks pass. Reusable native handles and 62 additive profiles are ready;
new profiles are unmeasured, no Stage 4 reference exists, and all old timings/
references are unchanged. Text-read attribution, pipelining, PAM/GSSAPI and CDC
remain later work. Keep this stage uncommitted and do not run benchmarks until
Anton authorizes them.

Stage 3 publication authorization, later on 12 November: Anton requested commit
and push of the complete native API stage, review fixes, documentation and saved
validation evidence to the fork's existing `master`. This supersedes earlier
uncommitted-only instructions for this delivery. Verify the completed receipt;
do not repeat tests or benchmarks just for publication. No package release is
authorized.

Review-fix authorization, later on 12 November: Anton requested fixing all six
Stage 3 findings. `docs/STAGE3_REVIEW_FIXES.md` records the corrections and new
evidence. Keep all changes uncommitted on `master`; no push or release is
authorized. This fix pass runs correctness/race/static/fuzz and regression
negative controls only; timing captures and immutable references stay intact.

Completed review-fix receipt: `validation/results/stage3-review-fixes-confirmation.json`.
Exact source `e6959af1…a6dfe15` has 1,222 Mac / 1,225 Linux passing leaves,
zero failures, no lost prior passes, race/vet/56 harness checks passing. The
full collector also passes all check/fuzz stages under --correctness-only;
its saved prior capture protects newly added pass identities. Restored defects
fail 2 cursor / 6 stream / 4 callback / 14 rollback assertions. An isolated
64 KiB packet regression passes; the container was removed. No benchmarks ran.

Historical Stage 3 review, later on 12 November: `docs/STAGE3_CODE_REVIEW.md` recorded six
pending P2 findings, with counterexamples in `results/stage3-review-20261112`.
Three concern driver error/packet handling; three concern validation/provenance/
rollback coverage. No production/harness fixes or benchmark reruns were made in
this review. Retained cursor pointers were investigated but not established as
an incremental leak. The subsequent authorized fixes above supersede that
pending status; the original counterexamples remain preserved.

Benchmark authorization, later on 12 November 2026: Anton explicitly authorized
running the benchmarks. This supersedes the earlier correctness-only restriction
for this measurement pass. Run the complete matching suite, preserve immutable
references and previous captures, record actual host conditions and update the
live report. Keep changes uncommitted; no commit/push or release is authorized.

Completed benchmark follow-up: `docs/STAGE3_BENCHMARKS.md` and
`validation/results/stage3-benchmark-confirmation.json`. Final full capture
`stage3-benchmark-final/capture.json` has 200 profiles × seven repetitions,
all stages exit zero, 1,196 ordinary passing leaves, eight skips, no lost passes,
46 harness tests passing and source `d40de51e…c37a2a4`. Driver/Go workloads are
unchanged from the initial Stage 3 correctness receipt; only reporting/audit
Python changed. Thirty-three original profiles exceed +5% time; four exceed
+20%; host conditions and performance acceptance remain qualified. Old timing
capture is preserved at `results/stage3-prebenchmark-history/current.json`.

Stage 3 authorization, 12 November 2026: Anton approved implementing per-unit
bulk results, streamed prepared parameters, server cursors and progress callbacks.
Work directly on `master`, leave changes uncommitted, preserve frozen workloads
and earlier captures, and run correctness/race/static checks only. No benchmark
execution, including smoke, commit/push or release is authorized for this stage.

Stage 3 final correctness receipt: `docs/STAGE3_NATIVE_APIS.md` and
`validation/results/stage3-confirmation.json`. Identical fingerprint
`b0fcfc8e76e78454c198168f8ebc2de60e7fc53830265597ff0cb019f02817b8`
has 1,196 Mac / 1,199 Linux unique passing leaves, zero failures, no lost Stage 2
passes, race/vet/44 harness checks passing. Timings remain historical and unchanged.
Keep native lifetime/cancellation/retry comments public and helpful. Remaining
API areas are interactive PAM, GSSAPI and optional CDC; performance work needs
an authorized measurement window. Do not commit or push until Anton requests it.

Session-stage publication authorization, 12 November 2026: Anton requested
committing and pushing the completed session foundations, all four review fixes
and their documentation/evidence to this fork's existing `master`. This supersedes
the stage's earlier uncommitted-only instruction for this delivery. Do not repeat
tests or benchmarks just for publication; verify the existing source receipt.
No package release or new performance run is authorized.

Stage 2 authorization, 12 November 2026: Anton approved the session-foundations
stage in `docs/STAGE2_SESSION_FOUNDATIONS.md`. Implement directly in this checkout
on `master`; leave the new stage uncommitted. The live desired-clean-pool test may
explicitly request the new opt-in policy, with an exact recorded contract migration
and the original baseline preserved. All other frozen workload bodies stay fixed.
Correctness only: no benchmark execution, including one-iteration smoke checks.

Publication authorization, 12 November 2026: Anton requested committing and
pushing the completed Go driver work to this fork's existing `master` branch.
This supersedes the uncommitted-only status for this delivery. It does not
authorize a package release, upstream submissions or new performance runs.

Validation restriction, 12 November 2026: Anton is heavily using the Mac and
requested correctness checks only. Do not run further performance benchmarks,
including benchmark smoke checks, during this pass. Preserve completed captures
and their contention qualifications; do not use them as performance acceptance.

Anton chose a dedicated MariaDB driver and authorized implementation. Use module
`github.com/aantich/go-mariadb`, package `mariadb` and SQL registration `mariadb`.
The earlier upstream-first recommendation was declined; do not send PRs or other
messages upstream. Keep this work uncommitted until requested. No package release
or publication is authorized. Historical baseline instructions follow; the
source-unchanged/rename restriction described the initial review only.

Use `validation/record.py` for current captures and `validation/verify.py` for
freshness. Original `run.py` and `check.py` remain frozen historical workloads.
The exact inverse identity adapter must reproduce original workload hashes.

# Stage 6 review corrections

User authorization, 12 November 2026: Anton requested fixing all seven findings
in `docs/STAGE6_CODE_REVIEW.md`. Implement permanent regressions and repeat
correctness/race/static/fuzz checks on Mac and isolated Linux. Do not run any
benchmark, smoke benchmark or profiling workload. Preserve earlier receipts and
timing/baseline artifacts. Keep all changes uncommitted on `master`; no push or
release is authorized.

# MariaDB Go driver work

User authorization, 11 November 2026: inspect the newly cloned Go fork, compare
with MariaDB, run relevant tests, create a proper correctness/benchmark suite,
record an unchanged-driver baseline and propose implementation stages. The
current pass does not authorize package publication or a driver/module rename.

Read `docs/MARIADB_REVIEW.md`, `docs/ROADMAP.md` and `validation/README.md` before
implementation. Work in the existing checkout, inspect/preserve changes first.
The inherited branch is `master`; the persistent `main` instruction in the
parent project specifically names the Haskell driver. Do not silently rename
this branch or module as part of a benchmark or correctness fix.

Keep the original driver at `e6232456bb6eb92a117563823ac6956937410b96` identifiable.
The first baseline under `validation/baselines/` is immutable, including failed
assertions, raw logs and benchmark workloads. Add new feature tests/benchmarks
in separate files. Fix actual driver code rather than changing assertions to
obtain green results. Label a deliberately new API contract as a gap.

Run/provision a real MariaDB before integration or performance measurement.
Use only task-owned Go schemas/accounts, keep credentials in ignored owner-only
files and never change shared-server global settings or application data for
test convenience. Missing fixture coverage is explicit, not a passing test.

Use `validation/run.py` for complete source-fingerprinted captures and
`validation/check.py` for freshness/baseline verification. Preserve Baseline,
Current, Change vs baseline columns and precise units. New machine/compiler/
server/network configurations require separate profiles; Go/Haskell samples
are not interchangeable speed comparisons. Documentation-only edits do not
justify repeating a complete benchmark capture.

The target is MariaDB only. Preserve protocol names MariaDB uses, including
mysql_native_password and shared wire identifiers. Do not claim inherited
metadata caching, ed25519 or Go 1.27 scanning as new fork improvements.

Write idiomatic Go and helpful public API/resource/error/lifetime comments.
Preserve database/sql ownership, cancellation and retry contracts. Returning
ErrBadConn after a potentially executed write can cause unsafe automatic replay.
Do not silently turn liveness-only pool checkout into a destructive reset.

Leave this initial review/test/plan pass uncommitted for review; no commit,
push or public Go release was requested.
