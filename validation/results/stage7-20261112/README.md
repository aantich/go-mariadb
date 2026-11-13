# Stage 7 evidence, 12 November 2026

Final executable inputs: `98ba80ed279200ff93c51231f27cd9568f56f8fe8d67b7b7335f079f0526635f`.
See [implementation record](../../../docs/STAGE7_COMPATIBILITY.md),
[matrix](../../COMPATIBILITY.md) and [confirmation](../stage7-confirmation.json).

* Full current-version final receipts are `stage7-mac-final-3` and
  `stage7-linux-final-3`: 1,637/1,640 PASS, 8/5 unchanged skips, no failures,
  race/vet and 99 harness tests passing. `publish.log` verifies/publishes the
  existing final Mac receipt without repeating tests; `verify.log` checks
  current freshness and immutable references.
* `stage7-matrix-confirmed/matrix.json` identifies all 12 final-source cells.
  Ten were freshly collected there; two complete exact-source cells were reused
  from earlier batches. Their paths and hashes are explicit. Every cell is
  independently checked by the actual `compatibility.validate` function.
  No incomplete/failed/stale-source cell is presented as current coverage.
* `source-first.json` and `source-readonly-fix.json` retain earlier input maps.
  The first full Linux capture found a history test writing into the read-only
  checkout. Further captures detected source changes while collector/harness
  corrections were being made. Those attempts and their raw outcomes remain in
  the earlier `stage7-*-final[-2]` and matrix directories. Correctness passing
  under an earlier harness is historical evidence, never relabelled as final.
* `stage7-development-20261112` contains initial compile/layout/controller
  probes, the first 10.11 GTID-list failure, raw synthetic-binlog evidence and
  the fixes' focused checks. The first matrix also exposed that real KDC tests
  need their own authentication fixture; the explicit older-server contract
  excludes that subpackage, while the complete current-version suite retains
  its mandatory real provider/KDC checks.
* `negative-controls.py.txt` restores decoder padding rejection and unsafe
  replay only in Go overlays. Both compile and fail the intended permanent
  regressions under race detection. No build failure counts as a control.
* `fuzz.json` and three logs record bounded decoder/sequence/value campaigns.
  Final code and Go test bodies match those executed; only Python harness/
  controller changes followed. The final confirmation corroborates that input
  difference explicitly. Counts are correctness exercise, not performance data.
* `portability.json` and logs record CGO-disabled tests plus Windows amd64
  root/CDC and Linux 386 CDC cross-compilation. No Windows/386 runtime claim.
* `toolchain-downloads.json` retains official Go archive URLs/checksums.
  Actual executable SHA256/version and runtime image IDs are in each profile.
  `server-sources.json` and attributed excerpts preserve MariaDB's empty-list
  padding rule and checkout identity; the GPL source license copy is linked.
* Batch scripts are saved as `.py.txt` so Go/Python source discovery never treats
  evidence copies as executable project inputs. Cell reproduction uses the
  maintained `validation/compatibility.py` CLI. `publish-matrix.py.txt` verifies
  all twelve cells and generates the matrix/current index without running tests.

No benchmark function, smoke benchmark or profiling workload ran. Original
references and historical timing bytes remain unchanged. Stage 6 was committed
and pushed as requested; this new stage remains uncommitted on `master`.
