# Stage 6 correction evidence, 12 November 2026

Current source: `69f760d553f6ec430157640a420190123790d4a636555d766126f468ab03c043`.
See [correction report](../../../docs/STAGE6_REVIEW_FIXES.md) and
[final confirmation](../stage6-review-fixes-confirmation.json).

* `source.json` freezes the executable source/test/CI input maps.
* `mac-final.log`, `linux-final.log` and adjacent final receipt directories retain
  full correctness/race/vet/89-harness captures. Linux uses the saved
  `linux-check.sh.txt` in a disposable container with a read-only source mount,
  isolated services and no published ports.
* `negative-controls.py.txt` creates replacement files and Go overlays only in
  this evidence directory. Its five Go controls must exit 1 with an expected
  assertion diagnostic, never a build error. Two isolated Python variants restore
  PATH-only discovery and the exited-leader guard. Each owns its child process
  groups, including cleanup after expected failures. See `negative-controls.json`.
* `fuzz-decode-final.log`, `fuzz-sequence-final.log` and
  `fuzz-values-final.log` are eight-second bounded correctness campaigns.
  Earlier fuzz runs remain developmental evidence, not final-source coverage.
* `standalone-linux-distribution.json` records the pinned setup action archive
  URL, verified SHA256 and extracted server/client/initializer hashes. The archive
  is mounted read-only at `/selected`; `standalone-linux-check.sh.txt` runs all
  15 live CDC cases using that explicit matching prefix. System OpenSSL creates
  fixture certificates. No MariaDB binaries or scripts were modified.
* Earlier standalone experiments are retained. The Darwin archive has absolute
  CI-host library paths and cannot run from an arbitrary temporary prefix through
  its shell initializer. The first Linux experiment overrode PATH/loader state
  too broadly, selecting bundled OpenSSL with an unavailable compiled-in config.
  These setup failures are not driver failures or successful CI evidence; the
  final unmodified Linux distribution run passes.
* `first-focused.log` precedes the permanent regression additions. Later focused
  logs expose an invalid synthetic rotation comparison and MariaDB's legitimate
  cached zero-position events. Corrected fixtures/state handling and all failed
  attempts remain visible; final receipts are separate.
* `server-sources.json` and two attributed `.cc.txt` excerpts preserve the local
  MariaDB checkout identity and the cache-position/binlog-EOF source rules.
  The source license copy is in the original review evidence directory.
* `portability.json`, per-check logs, `actionlint.log`, `verify.log` and
  `publish.log` record portability/static/publication verification. Cross-builds
  make no Windows/386 runtime claim.
* `historical-documents/` contains hash-matched copies of the seven mutable public
  documents and the earlier live correctness pointer listed in the original
  Stage 6 confirmation. Raw receipts, original
  review evidence and both historical confirmation files retain their bytes.

Mac reproduction, using existing private local SQL configuration and the isolated
authentication image:

```sh
GOCACHE=/private/tmp/go-mariadb-validation-20261111/go-cache \
  GOMAXPROCS=2 GOFLAGS= GOWORK=off GOPROXY=off PYTHONDONTWRITEBYTECODE=1 \
  python3 validation/auth_fixture.py -- python3 validation/cdc_fixture.py -- \
  python3 validation/correctness.py --output validation/results/new-unique-attempt
```

Publication used `--publish-existing --publish` after both exact-source profiles
passed, so it did not repeat tests or measurements. No benchmark, benchmark smoke,
profiling workload, commit, push or release ran during this correction pass.
