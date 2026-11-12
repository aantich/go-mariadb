# Native API review corrections

12 November 2026. Anton authorized fixing all six findings in the
[Stage 3 review](STAGE3_CODE_REVIEW.md). The corrections use the existing `master`
checkout, parent `b33f66b`. Anton subsequently authorized committing and pushing
the complete stage and its review corrections on 12 November. Receipts retain
their original pre-publication provenance; publication does not repeat tests or
measurements. No package release is included.

## Corrections

| Finding | Correction | Regression coverage |
| --- | --- | --- |
| R1 — Cursor materialization errors | Decode terminal ERR after cursor metadata before checking cursor status. Preserve MariaDB errno/SQLSTATE and keep a complete SQL error synchronized; malformed packets still retire the connection. | Live error 1242 across seven transport/metadata modes, repeated prepared execution and healthy reuse; malformed/valid error packets in legacy and deprecated-EOF peer modes. |
| R2 — Stream packet boundary | Reserve LONG_DATA's seven-byte command body and one spare byte when clamping chunks. Command bodies stay strictly below the configured packet maximum. | Buffered/streamed payload parity around the boundary, explicit/autodiscovered limits, wire assertions and a real isolated 64 KiB server. |
| R3 — Callback error classification | Join callback errors after `sql.Conn.Raw` and driver error classification finish. Application-supplied ErrBadConn cannot erase a terminal SQL error or retire a healthy connection. | Direct/wrapped callback sentinels with SQL success and error 1062; result/error identity, callback-state cleanup and subsequent Ping. Existing cancellation/no-replay checks remain required. |
| R4 — Full-run check enforcement | Share full-run command/environment and fixture policy between collection and verification. Include tagged protocol cases in race checks. Require checksummed raw logs to match test summaries and every benchmark sample. Save the previous capture to protect newer individual pass identities as well as original baseline tests. Preserve successful evidence outside ignored binary directories. Failed checks cannot replace the timing pointer. | Omitted/substituted/narrowed stages, inherited compilation flags/workspaces, skipped/missing native fixtures, skipped required fault subtests, vanished new API subtests, changed logs/cases/samples and the actual verifier's `race → true` counterexample are rejected. Synthetic collector tests exercise successful/failed receipts without running timings. |
| R5 — Measured endpoint provenance | Probe the configured TCP endpoint/account/schema before and after the full run. Reuse the correctness runner's private-credential JSON probe. Derive legacy comparison fields from those snapshots and reject drift or reference-profile mismatches. | Two-endpoint routing, mismatched comparison metadata, complete-but-different server profiles, missing snapshots and before/after changes are rejected. Real receipts retain actual endpoint snapshots. |
| R6 — Rollback assertions | Check exact original bulk values immediately after rollback and require the streamed row to be absent. Later writes cannot hide an accidentally committed transaction. | Changing ROLLBACK to COMMIT fails all fourteen transport cases. Original frozen workloads and baselines remain unchanged. |

The three production fixes add no per-row work or new round trips to ordinary
queries. The stream correction changes the chunk boundary only when constrained
by the configured maximum. Cursor errors avoid needless reconnection, and
callback errors no longer trigger false connection retirement. These are code
properties; no new throughput or allocation improvement is claimed.

Public API comments and the [user guide](USER_GUIDE.md) explain the error,
packet-limit and connection-lifetime behavior. The
[validation guide](../validation/README.md#full-capture-check-contract) documents
the `full-v1` command/raw-log/provenance contract. Earlier captures are retained
as historical evidence and do not satisfy that contract retroactively.

## Validation and evidence

Final executable input fingerprint:
`e6959af17eae99bc6ca106025097630ccd3a4ce7f69f23ec167aa6eb4a6dfe15`.

The [confirmation manifest](../validation/results/stage3-review-fixes-confirmation.json)
links exact-source Mac/Linux receipts, checksummed raw logs, the isolated packet
check and all regression negative controls. Both profiles use Go 1.27.4 and
MariaDB 12.3.3; exact host/server strings remain in their receipts. The wider
hosted toolchain/server matrix and application pilots remain future validation.

All **51 production/module inputs** and **71 workload/CI inputs** match across
the final Mac/Linux receipts and remain stable during each run. Both use race
detection, bounded test parallelism, all **56 harness checks**, and vet.

| Profile | Unique passing leaves | Failures | Skips |
| --- | ---: | ---: | ---: |
| macOS arm64, shared managed service | **1,222** | **0** | 8 |
| Linux arm64, isolated instrumented server | **1,225** | **0** | 5 |

Each profile adds **26** passing Go leaves; none of its earlier Stage 3 passes
was lost. The skips retain existing platform/fixture limits; required native
fixtures and fault subtests cannot skip. The isolated packet run separately
passes explicit and automatic packet-limit discovery on a 64 KiB server. No
shared-server global setting was changed. Actionlint and `git diff --check` pass.

The repaired full collector was also exercised on this exact source with
`--correctness-only`: all three ordinary suites, the complete tagged race suite,
vet, 56 harness checks and both eight-second fuzz campaigns exit zero. Its raw
logs, endpoint snapshots and preserved prior-pass reference verify successfully.
The full measurement verifier is tested with complete synthetic receipts and
negative controls; no new benchmark processes were executed.

The temporary negative controls restore each production defect independently or
replace the two rollback commands with COMMIT. They must fail the registered
regression assertions: **2 cursor**, **6 stream boundary**, **4 callback** and
**14 rollback** cases. They do not edit the repository's actual Go inputs.
An earlier stream negative control timed out because its peer predicted the
implementation's chunk count; that attempt is retained. The final peer replies
by command type, and the restored bug now fails explicit packet-size assertions.

The first Linux attempt passed driver checks but found that a new synthetic
Mac collector fixture inherited Linux's instrumentation-required environment.
Its test now explicitly selects its own fixture environment. The failed attempt
and subsequent successful intermediate receipts are preserved. No production
assertion or fixture requirement was weakened to hide that failure.

## Preserved timing evidence

The [live report](../validation/RESULTS.md) distinguishes current correctness
from the earlier measured source. The last timing pointer remains byte-identical
to `stage3-benchmark-final/capture.json`, SHA-256
`02fea12c04c5c42f1596bdb0c56ddd3839afb26a86f7e3e61f0b7e798adea7ed`.
Original and first-native reference hashes and workload bodies remain protected.
No benchmark function or smoke benchmark ran during this fix pass. Timing
acceptance, dedicated throughput baselines for the four new APIs, PAM/GSSAPI,
optional CDC and broader platform/application testing remain separate work.
