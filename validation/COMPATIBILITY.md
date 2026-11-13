# MariaDB compatibility and failure recovery

**All 12 compiler/server profiles pass.** Correctness only; readiness remains **Alpha**.

Source: `98ba80ed279200ff93c51231f27cd9568f56f8fe8d67b7b7335f079f0526635f`. Counts are unique passing/skipped test leaves, not benchmark samples.

| MariaDB tested | Go 1.25.14 PASS / SKIP | Go 1.26.11 PASS / SKIP | Go 1.27.4 PASS / SKIP |
| --- | ---: | ---: | ---: |
| 10.11.19 | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.25-mariadb10.11/capture.json) | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.26-mariadb10.11/capture.json) | [✅ 1,303 / 6](results/stage7-matrix-confirmed/go1.27-mariadb10.11/capture.json) |
| 11.4.13 | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.25-mariadb11.4/capture.json) | [✅ 1,219 / 6](results/stage7-matrix-refreshed/go1.26-mariadb11.4/capture.json) | [✅ 1,303 / 6](results/stage7-matrix-confirmed/go1.27-mariadb11.4/capture.json) |
| 11.8.9 | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.25-mariadb11.8/capture.json) | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.26-mariadb11.8/capture.json) | [✅ 1,303 / 6](results/stage7-matrix-confirmed/go1.27-mariadb11.8/capture.json) |
| 12.3.3 | [✅ 1,219 / 6](results/stage7-matrix-final/go1.25-mariadb12.3/capture.json) | [✅ 1,219 / 6](results/stage7-matrix-confirmed/go1.26-mariadb12.3/capture.json) | [✅ 1,303 / 6](results/stage7-matrix-confirmed/go1.27-mariadb12.3/capture.json) |

Every cell has **zero failures**, race detection and vet passing, exactly one owned restart,
cleaned-up resources and no lost earlier matching-profile passes. Integration fixtures and
new fault/capability cases cannot skip. Skips shown above belong to the inherited root suite.

Go 1.27 includes 84 additional direct row-scanning test leaves. Older compiler
profiles use the legacy scanning path; no passing case was lost within a matching profile.

**Platform scope:** these matrix clients run on macOS arm64; servers run in digest-pinned
Linux arm64 Docker images. The [complete current-version suite](RESULTS.md) separately
passes on Mac and isolated Linux, including real PAM/KDC/native-provider and instrumentation
coverage. The new CI declares all 12 Go/server combinations on Linux runners; no remote run
of this uncommitted CI change is claimed. Windows/386 cross-builds are not runtime coverage.

| Server feature | 10.11 / 11.4 | 11.8 / 12.3 |
| --- | --- | --- |
| Bulk unit results | Explicit request rejected; no rows changed | Native units verified |
| PARSEC | Absent in the profile plugin inventory | Real verified-TLS authentication passes |
| VECTOR | SQL rejection checked | Value roundtrip passes |

The matrix covers inherited SQL/protocol/CDC unit tests and selected integration suites,
including native bulk/stream/reuse/cursor/progress/reset/metadata, precise values and TLS/zlib.
Recovery tests distinguish no-send retry from partial/lost-response ambiguity, inspect real
database effects, handle lock timeout/deadlock, restart prepared connections and resume CDC
from persisted GTIDs across a killed source and schema change. The full 12.3 suite retains
its broader latest-feature/authentication contract; this matrix does not replace it.

[Stage 7 implementation and limits](../docs/STAGE7_COMPATIBILITY.md) ·
[Machine-readable current profiles](results/compatibility-current.json) ·
[Confirmation](results/stage7-confirmation.json) · [Run the suite](README.md)

Earlier failed/in-progress-source attempts remain preserved. Only complete successful
receipts with the same final inputs appear here; two exact-source cells were reused after
validation rather than repeated. Historical timings and baseline bytes remain unchanged.
This stage is uncommitted; no benchmarks, profiling, package release or new push occurred.
