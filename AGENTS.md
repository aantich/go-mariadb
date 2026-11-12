# Current authorization, 12 November 2026

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
