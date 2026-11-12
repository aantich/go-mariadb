# Stage 5 review corrections

**Publication follow-up:** Anton requested committing and pushing the complete
stage and corrections to `master`. Uncommitted statements and parent revisions
below describe the original validation context. The executable-input fingerprint
and saved evidence are verified for publication without rerunning checks.

12 November 2026. All three findings in the [review](STAGE5_CODE_REVIEW.md) are
fixed, with permanent regressions and matching-source correctness on Mac and
isolated Linux. Work remains uncommitted on inherited `master`. No benchmark,
smoke benchmark or profiling workload ran; earlier timing captures and immutable
references are preserved.

## Authentication errors keep secrets across method switches

The connection attempt retains its external-authentication sensitivity after
switching plugins. Server ERR messages are suppressed at the packet handler,
preserving numeric code and SQLSTATE. Peer-controlled plugin names and PEM block
types now use an internal diagnostic classification: after external credentials
have been used, the returned error retains its classification without retaining
the peer's prose. Application callback/provider errors keep their original
identity and message, including callbacks returning their own `MySQLError`.

`TestExternalAuthErrorSecrecy` has 16 passing cases: initial and switched PAM,
direct ERR, native switch followed by ERR or an unknown plugin, reflected unknown
plugin names, both inherited PEM parsers and exact callback-error preservation.
Each applicable wire probe verifies that its synthetic PAM response was actually
written before testing secrecy. Legitimate method switches remain supported.

## Native Kerberos work has its own capacity budget

`gssapi.NewClient` shares a default budget of **32 native contexts**. Admission
occurs before worker creation, honors cancellation, and holds capacity through
actual native cleanup. Canceling a SQL caller or returning from Close does not
prematurely free capacity while a native open, step or cleanup is still running.
The worker still owns all native resources and never owns the SQL transport.

`gssapi.NewProvider(maxContexts)` supplies an independent configurable budget.
Create it once and share `provider.NewClient` across the relevant connectors;
creating one per connection would bypass the intended shared bound. Public API
comments and the [guide](USER_GUIDE.md#kerberos-gssapi) explain scope, cancellation,
cleanup, zero-value behavior and why SQL pool limits alone are insufficient.

`TestProviderAdmission` holds open, step and cleanup separately, saturates a
two-context provider, rejects eight subsequent timed-out callers without new
native work, then verifies capacity recovery and exactly-once cleanup.
`TestProviderCapacity` verifies parallel contexts, independent budgets, invalid
configuration and pre-canceled requests. `TestProviderDefaultAdmission` covers
the shared default factory's bound. Existing resource/cancellation tests remain.

The bound can queue new handshakes while all slots are occupied. Connection
contexts control that wait; native retries still follow OS Kerberos policy.
Capacity is configurable for the deployment. This correctness change is not a
measured performance improvement or a claim that native calls are interruptible.

## Current captures require complete external-authentication provenance

The current correctness and `full-v2` validators reject absent provenance even
when both the metadata and raw event are omitted together. Synthetic verifier
fixtures now supply explicit matching metadata and events. They remain clearly
synthetic receipts, never measurements. Historical captures are preserved and
their passing identities are checked through the separate historical-reference
path; absence no longer silently upgrades an old or incomplete capture into
current evidence.

Permanent harness tests remove metadata, events or both through the actual
correctness summarizer and full-run validator, reconciling the changed log
checksum and parsed case output. Every omission is rejected. Duplicate markers
and a claimed historical label also cannot bypass the current contract.

## Final validation

Both profiles use executable-input fingerprint
`9d7c4b8c5b19e246660b0326d2cf32c8cf114e6c998a29cac5c7372d29da7807`,
covering 60 production/module/native files and 93 workload/CI/fixture inputs.

| Profile | PASS | FAIL | SKIP | Earlier Stage 5 passes preserved | Added leaves |
| --- | ---: | ---: | ---: | ---: | ---: |
| macOS arm64 / Go 1.27.4 / MariaDB 12.3.3 | 1,398 | 0 | 8 | 1,377 | 21 |
| Isolated Linux arm64 / Go 1.27.4 / MariaDB 12.3.3 | 1,401 | 0 | 5 | 1,380 | 21 |

Both complete correctness profiles pass race detection, vet and **77 harness
tests**, including real isolated PAM/Kerberos services and native resource
cleanup. No previously passing leaf was lost. CGO-disabled provider tests pass;
the Windows fallback cross-compiles, without claiming Windows runtime or SSPI
coverage. Formatting, diff checks, actionlint and evidence freshness pass.

Three isolated defect-restoration controls fail their intended new regressions:
resetting sensitivity at a switch, removing native admission, and restoring the
missing-provenance exemption. These are assertion failures, not compilation or
race-detector failures. Production bytes remain unchanged by the controls.

The [confirmation and artifact hashes](../validation/results/stage5-review-fixes-confirmation.json)
link both platform receipts, checks and negative controls. The
[live report](../validation/RESULTS.md) now records this source's correctness;
its timing tables remain explicitly historical. The timing pointer's SHA-256
remains `02fea12c04c5c42f1596bdb0c56ddd3839afb26a86f7e3e61f0b7e798adea7ed`.
Earlier attempts and the original review counterexamples remain preserved.
Owned containers and temporary authentication caches were removed.
