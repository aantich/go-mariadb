# Application integration results

**All eight version profiles and native Linux pass. Readiness: Alpha.**

GORM 1.31.2 / shared SQL dialect 1.6.0; sqlx 1.4.0. All connections use the dedicated MariaDB driver.

Source: `c2468c56caf4c77b64c30f1e3458759f45167759e9f2953cc8cc2feb9bd7608b`. Correctness only; no performance measurement.

| MariaDB | Go 1.25 PASS / FAIL / SKIP | Go 1.27 PASS / FAIL / SKIP |
| --- | ---: | ---: |
| 10.11.19 | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.25-mariadb10.11/capture.json) | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.27-mariadb10.11/capture.json) |
| 11.4.13 | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.25-mariadb11.4/capture.json) | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.27-mariadb11.4/capture.json) |
| 11.8.9 | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.25-mariadb11.8/capture.json) | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.27-mariadb11.8/capture.json) |
| 12.3.3 | [✅ 41 / 0 / 0](results/stage8-review-fixes-matrix/applications-go1.25-mariadb12.3/capture.json) | [✅ 41 / 0 / 0](results/stage8-review-fixes-app-smoke-correctness/capture.json) |

Native Linux / Go 1.27 / MariaDB 12.3: [✅ 41 / 0 / 0](results/stage8-review-fixes-linux/applications/capture.json).

Each profile requires race detection, both modules’ vet checks, exact module locks,
a separate offline CGO-free core consumer, one owned server restart and resource cleanup.
Required application cases cannot skip. TLS/compression workflows run on both transports.

**Platform scope:** darwin/arm64 matrix clients; supplementary native Linux private Unix-socket client.

Coverage includes migrations (including a schema upgrade), CRUD/batches/upsert/RETURNING,
exact decimals and unsigned values, UTF-8, JSON/BLOB/NULL/time, translated and original errors,
automatic timestamps, savepoints/nested rollback, managed session reset, cached statements, pool exhaustion/cancellation,
early Rows.Close, expiry, concurrent updates and committed/uncommitted state after restart.

These automated applications do not establish production workload, proxy/cluster failover,
Windows, or other framework/version compatibility. User application pilots remain required.

[Application guide](../docs/APPLICATIONS.md) · [Stage 8 and release checklist](../docs/STAGE8_APPLICATIONS.md) ·
[Core correctness and historical timing](RESULTS.md) · [12-cell core matrix](COMPATIBILITY.md)

Run `python3 validation/verify.py` to require this evidence alongside the core suites.
No benchmark or profiler was run for Stage 8. No package has been published or tagged.
