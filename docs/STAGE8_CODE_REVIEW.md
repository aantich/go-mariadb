# Stage 8 code review: three P2 and two P3 findings

**Resolution:** All five findings are fixed in the subsequent authorized
[correction pass](STAGE8_REVIEW_FIXES.md). The review and reproductions below
describe the original reviewed source; its sealed bytes remain archived.

Reviewed 14 November 2026 on `master`, parent
`2a5fe58e5b15f52db7a375e301c2c09fd6e20e34`. Executable input fingerprint remains
`9e8bdde9d1ea1d5c2f80cdb5c9f2edc3f891e7eb10bbd5862670e0cc8cab85de`.
This is a review/report pass. Production code and permanent tests are unchanged;
all implementation work remains uncommitted.

The review covers the optional GORM adapter, pinned application workflows,
transaction and connection ownership, cancellation/cache/restart tests, the
shared fixture-controller refactor, source/module/consumer verification, history,
cohort publication, CI and application documentation. All 620 sealed Stage 8
artifact hashes remain intact. The earlier green results describe the cases
they ran; they do not cover the counterexamples below.

## 1. P2: the GORM wrapper hides required optional dialect behavior

Location: [interface embedding](../integrations/gorm/mariadb.go#L25).

`dialect` embeds the narrow `gorm.Dialector` interface. Although its underlying
shared dialect implements `SavePoint`, `RollbackTo` and `Apply(*gorm.Config)`,
those methods are absent from the wrapper's method set. GORM discovers them
through interface assertions on the outer dialect, so delegation through the
embedded interface does not preserve them.

Two application behaviors break:

- `g.Transaction` inside another transaction returns `gorm.ErrUnsupportedDriver`
  before invoking the inner callback. Direct `SavePoint`/`RollbackTo` are also
  unavailable. The existing application tests exercise only a single transaction.
- GORM skips the shared dialect's default timestamp-precision initialization.
  Ordinary models with automatic `CreatedAt`/`UpdatedAt` receive values finer than
  the default DATETIME(3) columns, so the in-memory value changes after reading it
  back. Existing tests set their own timestamp in an explicit precision-6 field.

**Live reproduction:** An isolated MariaDB 12.3 fixture uses the dedicated driver
for both the wrapper and the shared-dialect control. The wrapper reports neither
optional interface, rejects the nested transaction and does not call its inner
callback. Its generated timestamp has `925836000` nanoseconds versus `925000000`
after reload. The shared-dialect control supports both interfaces, completes the
nested transaction and roundtrips its generated timestamp unchanged.

**Correction:** Preserve the shared dialect's optional methods, either through
concrete embedding or explicit forwarding, while retaining the custom transport
check and error translation. Add interface assertions, live savepoint/nested
rollback tests and automatic-timestamp roundtrips. Preserve a caller's explicit
`NowFunc` when forwarding configuration initialization.

Evidence: [live probe](../validation/results/stage8-review-20261114/gorm-live-probe.log)
and [probe source](../validation/results/stage8-review-20261114/gorm-probe.go.txt).

## 2. P2: native Linux application results have no pass-history protection

Locations: [native validation](../validation/applications_local.py#L32) and
[capture construction](../validation/applications_local.py#L76).

The native collector records no predecessor or lost-pass snapshot, and its
validator returns after checking current cases and server provenance. Docker
application history deliberately excludes native receipts, so that other path
does not supply protection either. The cohort publisher simply trusts the native
validator. A later Linux-specific passing case can disappear while the complete
application cohort still verifies successfully.

**Synthetic reproduction:** A valid earlier matching native profile includes
`TestLinuxApplicationRegression`. A later matching profile omits it while keeping
every frozen required case. Both native validation and a complete nine-cell
cohort accept the later profile. This demonstrates missing history enforcement;
it does not claim a real recorded Linux assertion has already been lost.

**Correction:** Add native profile identity, predecessor discovery/snapshots and
lost-pass checks, including independent validation from the publication path.
Keep historical command/inventory validation independent of later additions.
Cover missing/corrupted predecessors and a disappearing extra Linux-only leaf.

Evidence: [history control](../validation/results/stage8-review-20261114/native-history-control.json).

## 3. P2: framework expiry tests bypass the frameworks they name

Location: [prepared-handle expiry](../validation/apps/lifecycle_test.go#L114).

All three `sql`, `sqlx` and `gorm` expiry children create and execute a plain
`*sql.Stmt` directly through `db`. They never use the selected application client,
a sqlx prepared wrapper or GORM's prepared statement cache. The additional names
repeat the database/sql check and cannot detect framework-specific cache recovery
failures after connection lifetime expiry. The separate restart test does use
framework handles; it does not close this expiry-specific coverage gap.

**Live temporary-copy control:** Replacing `app := client(t, name, db)` with an
empty `application{}` removes all framework-client construction. Running only
the three expiry children against an owned MariaDB still passes all three with
race detection. The checkout itself was not modified.

**Correction:** Warm and reuse each client's actual prepared handle/cache across
expiry: standard `sql.Stmt`, sqlx named/prepared statement, and a cached GORM
query. Verify the connection changes and the same framework operation still
returns the expected value. A missing/nonfunctional framework client must fail
its corresponding case.

Evidence: [control](../validation/results/stage8-review-20261114/expiry-no-framework-control.json),
[raw events](../validation/results/stage8-review-20261114/expiry-no-framework.log)
and [temporary change](../validation/results/stage8-review-20261114/expiry-control-change.txt).

## 4. P3: native application timeouts discard the failing stage's output

Location: [subprocess capture](../validation/applications_local.py#L95).

Stage output is buffered by `subprocess.run` and saved only after it returns.
`TimeoutExpired` bypasses both log creation and the stage record; the `finally`
block writes a receipt without a phase, failure reason or the partial output
already available on the exception. A stalled build/test therefore loses the
diagnostic needed to investigate it. Compiler/preflight failures before the
current `try` block can also leave no structured failure receipt.

**Synthetic reproduction:** A timeout carrying a unique partial-output marker
produces only `capture.json`, `server-before.log` and `server.log`. The marker,
failed stage and failure phase are all absent. No database was started for this
control. This finding concerns evidence retention; the parent fixture's cleanup
remains in place.

**Correction:** Retain stage output during execution or preserve timeout output
on failure, and write sanitized phase/stage/status diagnostics for setup and
execution failures. Diagnostic write errors must not mask fixture cleanup.
Add a timeout control that requires the partial-output marker to survive.

Evidence: [timeout control](../validation/results/stage8-review-20261114/native-timeout-control.json).

## 5. P3: the application report hardcodes the matrix client platform

Location: [rendered scope](../validation/applications_cohort.py#L92).

The collector/validator accept Linux clients and the new CI matrix runs on
Linux, but the renderer always says “Matrix clients run on macOS.” Publishing
valid Linux receipts would therefore produce an incorrect platform claim. The
current saved report's Mac wording is correct for its current local receipts.

**Synthetic reproduction:** A complete accepted nine-cell cohort with Linux
compiler identities in every cell still renders the macOS claim.

**Correction:** Derive client OS/architecture scope from the verified profiles,
as the core compatibility report already does. Test Mac, Linux and permitted
mixed-client cohorts rather than putting the local capture's platform in a
reusable template.

Evidence: [rendering control](../validation/results/stage8-review-20261114/linux-render-control.json).

## Verification and limits

- All **122 existing Python harness tests** pass.
- The adapter's existing **race-enabled unit tests** pass.
- Live probes used private owned MariaDB containers/volumes; they were removed.
  Synthetic controls used temporary evidence trees and did not alter live pointers.
- No original driver/native source, permanent tests, baseline/reference bytes or
  sealed Stage 8 artifact changed. The original 68 runtime/native inputs remain
  unchanged, so this stage introduces no edits to the core driver's hot paths.
- No benchmark, benchmark smoke, profiling, full matrix rerun, commit, push,
  branch, tag or release occurred. Performance was not measured in this review.

The GORM optional-method regression should be fixed before recommending the
adapter for application pilots. The remaining findings concern validation and
reporting guarantees. All five remain pending; this pass applies no fixes.
