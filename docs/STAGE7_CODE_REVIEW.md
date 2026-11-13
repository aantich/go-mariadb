# Stage 7 review: two P2 findings and one P3 finding

Reviewed 13 November 2026 on `master`, parent `ec42783deb9727a529f3f9c29a80e7ed4d585e7a`.
Executable input fingerprint remains
`98ba80ed279200ff93c51231f27cd9568f56f8fe8d67b7b7335f079f0526635f`.
This pass reports findings; it does not fix production code or permanent tests.
Stage 7 and this review remain uncommitted.

The review covers the CDC padding/error changes, fault injector and transaction
assertions, native/transport/version cases, fixture ownership and cleanup,
history protection, CI, result publication and user documentation. No additional
driver correctness defect was found in this pass. The findings concern validation
and diagnostics; the recorded successful runs remain valid for their captured
source and cases.

## 1. P2: growing the required inventory can disable pass-history protection

Locations: [historical inventory validation](../validation/compatibility.py#L138)
and [prior-profile discovery](../validation/compatibility.py#L187).

`validate(..., current=False)` still requires historical integration receipts to
contain today's entire `INTEGRATION_REQUIRED` set. Adding a mandatory test in a
later stage invalidates every older receipt without that test. `prior_profile`
silently discards those receipts and returns no prior profile, allowing another
previously passing assertion to disappear undetected. This can happen during an
ordinary suite extension; no fabricated failure or metadata tampering is needed.

History discovery also scans only `results/stage7*/**/capture.json`, although
`--output` accepts other names. Captures under another valid output name, including
a future stage directory, receive no history protection on subsequent runs.

**Reproduction:** The synthetic probe records an extra passing leaf, then omits it
from a later matching profile. The unchanged-contract control correctly rejects
the omission. Adding one new mandatory passing case makes discovery return
`None` and validation accept the lost old leaf. An output directory without the
`stage7` prefix produces the same acceptance. These probes exercise the real
collector with temporary fixtures; they are not real server receipts.

**Correction:** Validate historical inventories against their recorded/versioned
contract, independently of later additions, while requiring the current inventory
for new captures. Discover eligible receipts by contract within the permitted
results tree, independently of a stage-specific filename. Preserve earlier
passing leaves and add regressions for inventory growth and output-name changes.

Evidence: [probe source](../validation/results/stage7-review-20261113/history-probe.py.txt)
and [results](../validation/results/stage7-review-20261113/history-probe.json).

## 2. P2: the standard verifier ignores the compatibility report

Location: [current verification entry point](../validation/verify.py#L9).

Stage 7 introduces `compatibility-current.json` and a live twelve-profile report,
but `validation/verify.py` checks only the existing correctness/performance
receipts and references. A missing, stale or incomplete compatibility cohort
does not affect its successful exit. Future source changes can therefore obtain
fresh full-suite verification while leaving the new compatibility claims stale.

The one-off publication/confirmation scripts did validate today's cohort. The
gap is the absence of that check from the maintained verification path.

**Reproduction:** A read-boundary mock presents the compatibility pointer as
missing, then as a stale empty cohort. The real verifier succeeds in both cases
without reading that pointer. The actual saved pointer was not changed and all
twelve actual profiles were separately validated during this review.

**Correction:** Add a maintained cohort validator and invoke it from the standard
verification/publication path. Require the exact twelve unique compiler/server
cells, current source, corroborated raw profile logs, hashes, counts and matching
summary metadata. Test missing, stale, incomplete, duplicate and altered cohorts.
Per-profile CI can continue validating its own cell separately.

Evidence: [probe source](../validation/results/stage7-review-20261113/verification-probe.py.txt)
and [results](../validation/results/stage7-review-20261113/verification-probe.json).

## 3. P3: fixture setup failures lose the actionable diagnostic

Locations: [Docker error handling](../validation/compatibility.py#L213)
and [outer failure recording](../validation/compatibility.py#L412).

The Docker helper discards failing command output. The controller then discards
even its sanitized operation name, retaining only an exception class such as
`RuntimeError`. If image pulling or another setup phase fails before the server
exists, the saved receipt has no stages or logs, and the console tells the user
to inspect a nonexistent `server.log`. Registry, networking, image-selection and
other setup failures become indistinguishable in the retained evidence.

**Reproduction:** A mocked image-pull failure through the real controller produces
exit 1, `error_type: RuntimeError`, empty stages/logs and no indication that pulling
the image failed. No real Docker resource or database was started.

**Correction:** Retain a safe phase/operation identifier and sanitized command
diagnostics for setup failures. Keep credentials out of argv and public output;
do not serialize private subprocess environments or arbitrary exception objects.
Make the console point to diagnostics that were actually saved, and add coverage
for failures before startup as well as during restart.

Evidence: [probe source](../validation/results/stage7-review-20261113/setup-failure-probe.py.txt)
and [results](../validation/results/stage7-review-20261113/setup-failure-probe.json).

## Verification and limits

Fresh checks pass: all **99 Python harness tests**, the complete CDC package with
race detection, actionlint, Go formatting, Python syntax and `git diff --check`.
All **360** previously sealed artifact hashes and **12** compatibility profiles
were verified against unchanged executable inputs. The earlier full receipts
remain **1,637 Mac / 1,640 Linux PASS**, zero failures; those full suites were not
rerun in this review. The focused checks do not cover the new counterexamples,
which are demonstrated separately above.

No benchmark, benchmark smoke run, fuzz campaign or profiling workload was run.
No new runtime performance conclusion is claimed. The original/reference bytes
and historical timing pointer remain unchanged. No implementation or permanent
test changes, commit, push, release or remote CI execution occurred. The report
and review evidence are the only additions inside the Go checkout.

[Review verification manifest](../validation/results/stage7-review-20261113/review.json)
records the checks, findings and preserved source/artifact identities.
