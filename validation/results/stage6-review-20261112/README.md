# Stage 6 review evidence

The probes use unchanged executable source `f1ae6b97…2e5370b`. Go files are saved
as `.go.txt` and selected through overlays; the separate Python probe is also
saved as `.txt`. They are outside production/permanent test discovery and source
fingerprinting. Their failures are intentional counterexamples, not accepted
correctness passes or changes to the original assertions.

The final reproduction commands run from the repository root:

```sh
go test -race -overlay validation/results/stage6-review-20261112/replication-overlay.json \
  -run '^TestReviewReplication' -count=1 -v .
go test -race -overlay validation/results/stage6-review-20261112/cdc-overlay.json \
  -run '^TestReviewCDC' -count=1 -v ./cdc
python3 validation/results/stage6-review-20261112/fixture-probes.py.txt
python3 validation/cdc_fixture.py -- go test -race \
  -overlay validation/results/stage6-review-20261112/live-overlay.json \
  -run '^TestReviewCDCServerCursorConfiguration$' -count=1 -v ./validation
```

Each command exits 1 on current source because it reproduces a pending finding.
The overlay JSON retains this checkout's absolute paths. Adapt those mapping
paths when reproducing in another checkout. Earlier probe transcripts and their
source copies remain alongside the final versions. The live attempt uses an
owned, disposable socket-only MariaDB; no shared-server settings are changed.
The process-group probe creates its own new session and kills that group in its
own `finally`, independently of the defective cleanup helper.

`existing-focused.log` and `existing-harness.log` record successful existing
checks. `freshness.log` verifies the saved source/reference receipts. The
expansion allocation finding is structural source analysis; no timing,
allocation-profile or benchmark measurement was performed.

Third-party packaging source copies are retained to verify the CI layout claim:

* `setup-{installer,starter}.ts.txt` and `setup-mariadb-versions.json` come from
  [actions-setup-mysql v1.56.2](https://github.com/shogo82148/actions-setup-mysql/tree/v1.56.2).
  Its original license is `setup-action-LICENSE.txt`.
* `setup-mariadb-build.sh.txt` comes from the
  [distribution release tag](https://github.com/shogo82148/build-mysql/tree/mariadb-12.3.3-20260910082800).
  Its original license is `setup-build-LICENSE.txt`. `setup-build-paths.txt` is
  discovery metadata from that repository's then-current main, not evidence of
  the release layout by itself.
* `server-12.3.3-install-*.cmake.txt` comes from
  [MariaDB server 12.3.3](https://github.com/MariaDB/server/tree/mariadb-12.3.3).
  Its original GPL license is `server-12.3.3-COPYING.txt`.

These copies are reference evidence and are not compiled into the driver.
`capture.json` records the tested input fingerprint and checksummed artifacts.
