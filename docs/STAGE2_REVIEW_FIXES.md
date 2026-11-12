# Session foundations: review corrections

12 November 2026. Anton authorized fixing all four findings in the
[session review](STAGE2_CODE_REVIEW.md). The corrections, regression tests and
fresh correctness results were prepared on `master`, parent `d7edf02`.
Anton subsequently authorized committing and pushing the completed stage on
12 November. The receipts retain their original pre-publication provenance;
publication does not repeat measurements. No benchmarks, benchmark smoke checks
or package release were performed.

## Corrections

| Review | Correction | Verification |
| --- | --- | --- |
| R1 — Discarded metadata materialization | Extended metadata discard validates borrowed packet slices without building a column array or copying discarded names/values. Retaining and discarding consumers share attribute decoding and fixed-tail validation. Common attribute IDs use a duplicate-detection bit mask; large future IDs use a bounded map. | Prefix truncation, duplicate IDs across both representations, 1,024-attribute bound, metadata byte budget, legacy/deprecated EOF, subsequent-packet boundary and malformed-stream retirement regressions pass. |
| R2 — Unverified check commands | Collection and publication share exact race/tag/package, harness-discovery and vet command contracts. The runner records `GOMAXPROCS=2`, empty `GOFLAGS` and `GOWORK=off`. Publication and rendering derive successful-check claims from those validated contracts. | Removing race/tag flags, narrowing test/package scope, substituting commands and changing the recorded Go flag environment are rejected. Six additional counterexamples using the actual final receipt are rejected. |
| R3 — Missing server provenance | The runner queries the configured TCP endpoint/account/schema before and after checks. Checksummed JSON snapshots record the actual MariaDB version, instrumentation, packet/durability settings and probe-session encoding/SQL-mode defaults. Missing/mismatched snapshots or changed settings prevent publication. | Both real platforms retain matching snapshots; negative controls cover absent provenance, mismatched raw/recorded values, settings drift and disabled required instrumentation. The latest report displays its own profile. |
| R4 — Git dependency in source exports | Revision lookup records an explicit unavailable status for absent Git, missing repository metadata or lookup failure. Source/workload fingerprints remain mandatory. | The complete suite passes from an identical Mac source export with Git installed and no `.git` metadata. Its revision is null with Git exit 128 recorded; unit tests cover the other lookup outcomes. |

The metadata correction removes identifiable allocation work; no throughput or
runtime allocation measurements are claimed. Compiler diagnostics confirm that
the small shared fixed-tail validator is inlined in the ordinary column reader.
Returned native metadata retains its ownership and type/scanner behavior.
The [guide](USER_GUIDE.md#native-column-metadata) now explicitly explains why
`Describe` returns `TINYINT` while configured boolean scanning exposes `BOOLEAN`
through `ColumnTypes`.

The first live provenance probe exposed MariaDB's boolean-system-variable JSON
serialization: unquoted ON/OFF made the response invalid JSON. The query forces
those three values to numbers using arithmetic. Credentials stay in the private
subprocess environment; argv, snapshots and published logs contain no password.
A failed snapshot query retains an unsuccessful receipt with a redacted
diagnostic and cannot be published.

## Exact-source validation

Input fingerprint:
`8df163063b8be05a12cc41bd2430d97a1ab121d26ef1a19d4e980dca107fa80e`.
All **45 production/module inputs** and **64 workload/CI inputs** match across
the three successful runs and remain stable during each run. All use Go 1.27.4
and MariaDB 12.3.3; exact OS/server strings and settings are retained in receipts.

| Profile | Unique passing leaves | Failures | Skips | Race / harness / vet |
| --- | ---: | ---: | ---: | --- |
| macOS arm64, working checkout | **1,070** | **0** | 8 | Pass; all 44 harness tests pass |
| Linux arm64, isolated instrumented server | **1,073** | **0** | 5 | Pass; all 44 harness tests pass |
| macOS arm64, source export without Git metadata | **1,070** | **0** | 8 | Pass; all 44 harness tests pass |

No earlier passing leaf was lost, including the prior session-foundations
Mac/Linux results. Seven new Go leaves include ordinary fuzz seeds; nine new
Python tests bring the harness total from 35 to 44. No timed fuzz campaign was
run. Actionlint 1.7.12 and `git diff --check` also pass.

The skips retain the prior profile limits: shared-service saturation, three
inherited DSN fuzz seeds and unavailable server zstd negotiation on both systems;
Mac additionally skips the inherited platform-specific context-begin case,
connection attributes with performance_schema OFF and isolated instrumentation.
The latter two run on Linux with instrumentation enabled. The shared Mac's
global settings were not changed. A wider hosted server/toolchain matrix was not
executed in this pass.

The [confirmation manifest](../validation/results/stage2-review-fixes-confirmation.json)
links the [Mac receipt](../validation/results/stage2-fixes-mac-final/capture.json),
[Linux receipt](../validation/results/stage2-fixes-linux-confirmed/capture.json),
[source-export receipt](../validation/results/stage2-fixes-source-export/capture.json)
and [negative controls](../validation/results/stage2-fixes-controls/negative-controls.json).
Each evidence directory includes raw logs and checksums. The compiler diagnostic
is in [the control evidence](../validation/results/stage2-fixes-controls/compiler-inlining.log).

Two unsuccessful Linux setup profiles remain preserved. The first offline image
lacked dependency archives and failed before driver tests. With the exact modules
mounted read-only, the second reached the suite but the frozen external timeout
test received an immediate network-unreachable error in the offline network.
The final run restores the earlier routed Linux profile, publishes no server
port and keeps dependency fetching disabled. Its database and checkout mounts
remain isolated/task-owned and read-only respectively; the container is limited
to two CPUs and 4 GiB and is stopped after checking. No driver/test assertion was
changed to accommodate either environment failure.

## Live report and preserved references

The [live results](../validation/RESULTS.md) and
`validation/results/correctness-current.json` now identify the corrected source.
`validation/verify.py` passes with required command/server provenance checks,
raw-log validation, original workload/contract-migration protection and immutable
reference checks. The report is generated from verified correctness and the
unchanged historical performance capture.

The old timing capture remains byte-identical, SHA-256
`5fa0ebf51e2ddc51b555ff1d1e5aab5e28a976247c1419f079d95c778d0e0b4c`.
No benchmark baseline was advanced. Performance acceptance and the remaining
native APIs on the [roadmap](ROADMAP.md) remain open. Continue correctness-only
validation until Anton authorizes performance runs.
