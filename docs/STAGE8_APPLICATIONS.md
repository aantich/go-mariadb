# Stage 8: application integration and Alpha preparation

Anton approved this stage on 14 November 2026. Work remains uncommitted on
`master`. Benchmarks, smoke benchmarks, profiling and publishing are deferred.
The Stage 7 review corrections and all historical references remain preserved.

## Implementation

The separate optional `integrations/gorm` module fixes GORM's concrete upstream
error-type assumption. It adds portable duplicate/foreign-key identities while
preserving original MariaDB errors, rejects pools using the wrong transport,
and handles DSN versus existing-pool ownership explicitly. It uses the shared
SQL generator rather than copying its migration/query implementation. The core
module gains no application-framework dependencies.

`validation/apps` pins GORM 1.31.2, shared dialect 1.6.0 and sqlx 1.4.0 in its own
module. Full graph sums and local source replacements are corroborated in each
receipt. Transitive SQLite/PostgreSQL test modules in that graph are dependency
metadata, not tested or supported databases.

Required applications cover migrations/schema upgrade, CRUD, batch/upsert,
unsigned BIGINT/DECIMAL/UTF-8/JSON/BLOB/NULL/microsecond time, original and portable
error identity, rollback and MariaDB RETURNING. Lifecycle checks exercise
database/sql, sqlx and GORM under pool exhaustion, active cancellation, early
Rows.Close, expiry and concurrent updates. Managed-session tests check isolation
across frameworks. Restart tests keep actual standard/sqlx/GORM statement caches
and an uncommitted transaction open, then require committed state to survive,
uncommitted state to disappear, and cached queries to recover.

A frozen application command/inventory/dependency contract, raw event parsing,
attributed identity/restart markers, source/log/module hashes and protected
matching-profile pass history establish the eight Go 1.25/1.27 × MariaDB
10.11/11.4/11.8/12.3 cells. Native Linux repeats the same applications through an
owned socket server. Race, vet, lock resolution and a separate CGO-free offline
consumer are required. The maintained application cohort is an additional
`verify.py` gate; the full core suite and twelve-cell core matrix stay required.
The GitHub workflow declares the eight application cells independently.

## Validation and review

The subsequent [review corrections](STAGE8_REVIEW_FIXES.md) resolve all five
findings and refresh the live reports. The initial results below remain
historical evidence for the original Stage 8 source.

See the [current application report](../validation/APPLICATIONS.md),
[current core matrix](../validation/COMPATIBILITY.md), and
[live core correctness](../validation/RESULTS.md). These reports link raw
receipts. The [final confirmation manifest](../validation/results/stage8-confirmation.json) records the exact source,
commands, Mac/Linux/core/application profiles, harness checks and cleanup.
Failed development attempts remain saved; they are not counted as acceptance.
CI configuration is checked locally; no remote CI run is claimed before a push.

The first live application run corrected test setup for negotiated tracking and
sqlx's explicit batch column list. The first Linux attempt exposed a container
module-cache path error before compilation. These are retained as failed
development attempts, not hidden by overwriting the output directory.

Initial Stage 8 source was `9e8bdde9d1ea1d5c2f80cdb5c9f2edc3f891e7eb10bbd5862670e0cc8cab85de`
(71 driver/module/native inputs and 129 test/harness/CI inputs). Core correctness
passes **1,637 Mac / 1,640 Linux** unique Go leaves with **zero failures**, the
same **8/5 profile skips**, race/vet and **122 harness tests** on each platform.
The eight versioned application profiles and native Linux each pass **31 leaves,
zero failures and zero skips**. Application counts are separate from core counts.
All twelve core version/compiler profiles pass at that same source, with no lost
matching-profile passes. Go 1.25/1.26 cells have 1,219 passing leaves and six skips;
Go 1.27 cells have 1,303 passing leaves and six skips. Required integration cases
cannot skip; those six belong to the inherited root suite.

The review strengthened the foreign-pool regression to prove rejection before
any dial, rather than accepting a later connection error. Isolated source copies
with error translation or the pool guard removed fail the intended tests; the
unchanged copy passes. Harness controls reject missing cases/restart markers,
stale/altered module sources, consumer proofs, commands, logs, history or cohorts.

Final Linux checks use an identical private source export on the container's
filesystem to avoid shared-filesystem traversal delays. An earlier archive added
macOS metadata sidecars; its differing fingerprint excludes it from acceptance.
The accepted export disables that metadata and matches the checkout exactly.
Earlier source-changing attempts are also excluded and preserved. All 192
Stage 7 review-fix artifact byte sequences remain available, with archived copies
of changed live documents. Historical timing/reference bytes remain unchanged.

## Release gates still open

Readiness stays **Alpha**. This stage adds automated application workflows;
it does not establish real production use or measured performance for new APIs.

- Run pilots in real applications with their schema, authentication and proxy/
  topology settings. Cluster failover, Windows native authentication and wider
  framework versions remain outside the recorded scope.
- Complete the deferred [performance runbook](STAGE4_MEASUREMENT_PLAN.md) in an
  authorized idle window. Historical timings remain historical; new native
  reuse/cursor/upload/CDC/application paths have no accepted current timings.
- Review the public diff, package documentation, license attribution and release
  scope. The core MPL-2.0 license and upstream attribution are retained. Optional
  dependencies keep their own licenses; this is not a license audit.
- Select and approve a version, for example `v0.1.0-alpha.1`, and public release
  metadata. No version or release is created by this stage. Before publishing
  the optional module, replace its development `v0.0.0` core requirement with
  the real core version and use the nested module's matching tag prefix
  `integrations/gorm/v...`. Consumers ignore dependency `replace` directives.
- After the approved commit/push, require remote CI to pass. Verify module-proxy
  retrieval and a fresh consumer without local replacements after the approved
  tag. The current offline export check deliberately does not claim this.

[Application guide and migration examples](APPLICATIONS.md) ·
[Validation commands](../validation/README.md) · [Roadmap](ROADMAP.md)
