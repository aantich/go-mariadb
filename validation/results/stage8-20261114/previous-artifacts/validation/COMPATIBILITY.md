# MariaDB compatibility and failure recovery

**All 12 compiler/server profiles pass.** Correctness only; readiness remains **Alpha**.

Source: `a16c00cf01a35b193ab251edbadb1a04ece062906b8a3f12bffbabd6cee449f1`. Counts are unique test leaves.

| MariaDB tested | Go 1.25 PASS / SKIP | Go 1.26 PASS / SKIP | Go 1.27 PASS / SKIP |
| --- | ---: | ---: | ---: |
| 10.11.19 | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.25-mariadb10.11/capture.json) | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.26-mariadb10.11/capture.json) | [✅ 1,303 / 6](results/stage7-review-fixes-matrix-refreshed/go1.27-mariadb10.11/capture.json) |
| 11.4.13 | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.25-mariadb11.4/capture.json) | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.26-mariadb11.4/capture.json) | [✅ 1,303 / 6](results/stage7-review-fixes-matrix-refreshed/go1.27-mariadb11.4/capture.json) |
| 11.8.9 | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.25-mariadb11.8/capture.json) | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.26-mariadb11.8/capture.json) | [✅ 1,303 / 6](results/stage7-review-fixes-matrix-refreshed/go1.27-mariadb11.8/capture.json) |
| 12.3.3 | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.25-mariadb12.3/capture.json) | [✅ 1,219 / 6](results/stage7-review-fixes-matrix/go1.26-mariadb12.3/capture.json) | [✅ 1,303 / 6](results/stage7-review-fixes-matrix/go1.27-mariadb12.3/capture.json) |

Every cell has **zero failures**, race detection and vet passing, one owned restart,
cleaned-up resources and no lost earlier matching-profile passes. Required integration
cases cannot skip. The displayed skips belong to the inherited root suite.

**Platform scope:** darwin/arm64 clients; linux/arm64 Docker servers.

The [complete current-version suite](RESULTS.md) separately covers real PAM/KDC/native
providers and instrumentation on Mac and isolated Linux. The version matrix covers
SQL/protocol/CDC units, precise values, native bulk/stream/reuse/cursor/progress/reset/
metadata policies, verified TLS/zlib, transport faults, deadlocks, lock timeouts,
uncertain COMMIT, pool restart and explicit CDC resume across schema changes.

| Server feature | 10.11 / 11.4 | 11.8 / 12.3 |
| --- | --- | --- |
| Bulk unit results | Request rejected without changing rows | Native units verified |
| PARSEC | Absent in the profile plugin inventory | Verified-TLS authentication passes |
| VECTOR | SQL rejection checked | Value roundtrip passes |

Go 1.27 has 84 additional direct row-scanning leaves; older compilers use the legacy path.
Exact compiler identities and patch versions are in the linked receipts. This is Alpha
validation, not certification of production applications or failover topologies.

Run `python3 validation/verify.py` to check both current correctness and this complete
cohort. Historical commands and passing leaves survive inventory and output-name changes.
Missing, stale, incomplete or altered current compatibility evidence fails verification.

[Stage 7 implementation and limits](../docs/STAGE7_COMPATIBILITY.md) ·
[Review corrections](../docs/STAGE7_REVIEW_FIXES.md) ·
[Machine-readable profiles](results/compatibility-current.json) · [Run validation](README.md)

Earlier failed/source-changing attempts remain preserved. No benchmarks, smoke runs or
profiling were performed for these corrections; historical timing/reference bytes are unchanged.
New corrections remain uncommitted. No new remote CI run or package release is claimed.
