# Application integration results

**All eight version profiles and native Linux pass. Readiness: Alpha.**

GORM 1.31.2 / shared SQL dialect 1.6.0; sqlx 1.4.0. All connections use the dedicated MariaDB driver.

Source: `9e8bdde9d1ea1d5c2f80cdb5c9f2edc3f891e7eb10bbd5862670e0cc8cab85de`. Correctness only; no performance measurement.

| MariaDB | Go 1.25 PASS / FAIL / SKIP | Go 1.27 PASS / FAIL / SKIP |
| --- | ---: | ---: |
| 10.11.19 | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.25-mariadb10.11/capture.json) | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.27-mariadb10.11/capture.json) |
| 11.4.13 | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.25-mariadb11.4/capture.json) | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.27-mariadb11.4/capture.json) |
| 11.8.9 | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.25-mariadb11.8/capture.json) | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.27-mariadb11.8/capture.json) |
| 12.3.3 | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.25-mariadb12.3/capture.json) | [✅ 31 / 0 / 0](results/stage8-matrix/applications-go1.27-mariadb12.3/capture.json) |

Native Linux / Go 1.27 / MariaDB 12.3: [✅ 31 / 0 / 0](results/stage8-linux-accepted/applications/capture.json).

Each profile requires race detection, both modules’ vet checks, exact module locks,
a separate offline CGO-free core consumer, one owned server restart and resource cleanup.
Required application cases cannot skip. Matrix clients run on macOS; the supplementary
Linux client uses its own private Unix-socket server. TLS/compression workflows run on both.

Coverage includes migrations (including a schema upgrade), CRUD/batches/upsert/RETURNING,
exact decimals and unsigned values, UTF-8, JSON/BLOB/NULL/time, translated and original errors,
transaction rollback, managed session reset, cached statements, pool exhaustion/cancellation,
early Rows.Close, expiry, concurrent updates and committed/uncommitted state after restart.

These automated applications do not establish production workload, proxy/cluster failover,
Windows, or other framework/version compatibility. User application pilots remain required.

[Application guide](../docs/APPLICATIONS.md) · [Stage 8 and release checklist](../docs/STAGE8_APPLICATIONS.md) ·
[Core correctness and historical timing](RESULTS.md) · [12-cell core matrix](COMPATIBILITY.md)

Run `python3 validation/verify.py` to require this evidence alongside the core suites.
No benchmark or profiler was run for Stage 8. No package has been published or tagged.
