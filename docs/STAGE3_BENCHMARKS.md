# Stage 3 benchmark follow-up — 12 November 2026

Anton authorized performance measurement after the initial correctness-only stage.
The final declared confirmation capture uses all 200 existing profiles, seven
independent process repetitions per profile, GOMAXPROCS=4 and 200 ms Go benchmark
calibration. The original 184 profiles retain their unchanged-upstream reference;
the sixteen matched aggregate-bulk profiles retain their first native reference.
No reference was overwritten or advanced.

Production and Go workloads are unchanged from the initial Stage 3 correctness
receipts. The measurement/reporting source changes only the reporting/audit
harness: show the resolved managed-session contract accurately, and check every
historical passing identity against all raw events while counting only leaves in
summary totals. Container and child failures, missing events and nonzero test
process exits remain failures. Both harness regressions have tests.

The existing timed suite covers ordinary SQL/scanner/connection operations,
metadata caching and aggregate bulk alternatives. It does not yet define separate
throughput baselines for unit-result bulk, io.Reader uploads, cursor fetch or
progress callbacks. Their correctness/resource checks remain distinct.

Host activity was recorded rather than described as idle. Per-family ps snapshots
are not continuous monitoring or controls that establish the cause of a timing
difference. Profile-level regression signals remain explicit.

The original pre-stage timing capture is preserved in
validation/results/stage3-prebenchmark-history/current.json with SHA-256
5fa0ebf51e2ddc51b555ff1d1e5aab5e28a976247c1419f079d95c778d0e0b4c.
Raw measurements and unsuccessful/incomplete attempts remain separately retained.

## Final result

The declared confirmation is
[`20261112T105413171129Z`](../validation/results/stage3-benchmark-final/capture.json).
Its source fingerprint is
`d40de51ee6e045d07c242a9af50e0356a6cd5dd7e2d362570b9df7b1cc37a2a4`:
**51 production/module inputs and 67 workload/CI inputs**. All production files
and all Go workloads match the initial Stage 3 correctness receipts. Only
`validation/record.py` and its reporting/audit regressions changed afterward.

All **200 profiles × 7 repetitions = 1,400 samples** are present, every full-run
stage exits **0**, and inputs remain stable. Ordinary unique leaves are
**1,196 PASS / 0 FAIL / 8 SKIP**; all 1,196 earlier Stage 3 passing leaves remain
passing. Race checks, vet, **46 harness tests**, both existing eight-second fuzz
campaigns and benchmark builds pass. The prior exact-driver Linux receipt remains
1,199 passing leaves; Linux was not used for these timing comparisons.

The [confirmation manifest](../validation/results/stage3-benchmark-confirmation.json)
checksums both full captures/logs and the
[profile-level analysis](../validation/results/stage3-benchmark-analysis.json).
The [live report](../validation/RESULTS.md) now displays the fresh full result;
`validation/verify.py` verifies source freshness, original workload migrations,
all immutable references and complete repetitions.

## Performance observations

Of the **184 original profiles**, **33** exceed +5% elapsed time versus unchanged
upstream, **4** exceed +20%, and **7** improve by more than 5%. These are
investigation thresholds, not statistical or causal conclusions. **21 profiles**
exceed +5% in both complete captures. The first capture had 30 over +5% and ten
over +20%; retaining both captures exposes the variability.

| Selected unchanged workload | Upstream baseline ms/op | Current ms/op | Change vs upstream |
| --- | ---: | ---: | ---: |
| Prepared narrow read, 1,000 rows | 0.2266 | 0.2333 | +3.0% |
| Prepared 32-integer read, 1,000 rows | 0.4961 | 0.5046 | +1.7% |
| Text narrow read, 1,000 rows | 0.5123 | 0.6563 | +28.1% |
| Text BLOB read, 1 MiB, owned scan | 2.3891 | 3.0230 | +26.5% |
| Text BLOB read, 1 MiB, borrowed scan | 2.2694 | 3.0204 | +33.1% |
| Compressed narrow read, 1,000 rows | 1.2006 | 1.1255 | -6.3% |
| Prepared sequential writes, 100 rows | 3.6070 | 3.6762 | +1.9% |
| Text batch writes, 100 rows | 0.4443 | 0.4380 | -1.4% |

Text narrow and text BLOB reads retain substantial signals. Compressed narrow
reads varied markedly: +48.7%/+62.5% in the first typed/borrowed capture, versus
−6.3%/−3.4% in the confirmation. This pair is not treated as a confirmed timing
regression. Its extra allocation counts versus upstream were already present in
the preceding fork capture. In the confirmation, the TLS owned-BLOB profile has
one additional median allocation versus that preceding capture; two other
profiles have one fewer. No attribution to a code change is established.

Relative to the previous contention-qualified fork capture, 152 profiles improve
by more than 5% and five exceed +5%. These observations do not establish that the
new native APIs caused those differences.

| Rows per transaction | Current prepared loop ms/op | Current aggregate native bulk ms/op | Prepared/native ratio |
| --- | ---: | ---: | ---: |
| 1 | 0.2405 | 0.2783 | 0.86× |
| 16 | 0.7856 | 0.3096 | 2.54× |
| 100 | 3.7646 | 0.4662 | 8.08× |
| 1000 | 35.8005 | 2.8611 | 12.51× |

These compare API alternatives on this driver. Each includes the same transaction,
deletion and count/sum/content verification. Text/TSV and parameter matrices are
prebuilt, native encoding is timed, native prepare/close is transient and the
prepared control reuses its handle. At 1,000 rows the prebuilt text batch takes
2.6742 ms; the full live table includes all four alternatives. Native bulk is
slower than the prepared control for a single row. No universal speedup or
cross-language comparison follows from these measurements.

## Audit correction and retained attempts

The initial attempt was interrupted during its first benchmark process to correct
an obsolete caption describing the resolved clean-session assertion as a gap.
The [first complete measurement](../validation/results/stage3-benchmark-first/capture.json)
then finished all 1,400 samples and all test processes successfully, but its
historical-pass audit reported nine filtered container names twice across the
two references as missing. Its original exit **1** and raw samples remain intact.
The [independent raw-event audit](../validation/results/stage3-benchmark-first/pass-identity-audit.json)
finds no actual missing/failing historical identity.

The guard now checks both container and leaf statuses from raw events, while
summary counts stay leaf-only. Missing containers, failed containers and failed
children remain rejected. The final confirmation was declared before measuring;
it is the published capture, including its slower profiles. No numbers were
chosen per profile from whichever run looked better.

No commit, push, package release or baseline advancement was performed.

Public evidence directories contain byte-identical JSON/log copies of the local
execution directories. Ignored benchmark executables are omitted; raw command
paths still identify where the actual processes ran.
