# Stage 5 review: three findings (corrected)

**Follow-up:** All three findings are fixed. The
[correction record and new evidence](STAGE5_REVIEW_FIXES.md) describe current
behavior; this document preserves the original findings and counterexamples.

12 November 2026. Reviewed the uncommitted authentication changes against
`527b5b9577dedfce03ee1fa70ea22a7ed1f94a80`, including the core exchange,
PAM/GSSAPI policies, native ownership, public comments, isolated fixtures,
CI, collection and validation. All three findings below were pending at review time.

Production and workload inputs are unchanged at fingerprint
`c65a2f7f932395d71b48d621db4ea827a3f21a947600937d2bb3e6cee652171a`.
This pass adds review documentation and counterexamples only. No benchmark,
smoke or profiling workload, commit or push was performed.

| Priority | Finding | Confirmation |
| --- | --- | --- |
| P2 | External-authentication secrets can appear in errors after a plugin switch | Two race-enabled mock-wire counterexamples fail the secrecy assertion |
| P2 | Canceled callers can accumulate native Kerberos work beyond the connection limit | Eight sequential canceled callers leave eight held native opens outstanding |
| P2 | Current correctness publication can accept missing external-authentication provenance | The actual validator accepts an otherwise complete current receipt with metadata and its raw marker omitted |

## P2: keep error redaction across the entire authentication exchange

The [terminal error handler](../auth.go#L169) suppresses server prose only when
the **currently active** plugin implements `sensitiveAuth`. An auth switch
replaces that plugin. After a protected PAM callback has sent a response, a
subsequent switch to `mysql_native_password` followed by an ERR therefore returns
the peer's message verbatim. If that message echoes the earlier password or OTP,
the secret reaches the application's returned error and potentially its logs.

There is a second route: [unknown-plugin diagnostics](../auth.go#L124) quote the
server-controlled plugin name. A switch name reflecting the preceding response
exposes it without requiring an ERR packet or a successful new plugin.

The [review probe](../validation/results/stage5-review-20261112/secret-switch_test.go.txt)
actually writes a synthetic PAM response through the ordinary auth packet writer,
then exercises both switch sequences against the unmodified production code.
Both fail under race detection; the
[log](../validation/results/stage5-review-20261112/secret-switch.log) contains only
a synthetic sentinel, not real credentials. The direct-ERR regression passes
because it never switches away from the sensitive plugin.

**Correction:** retain sensitivity for the full exchange once an external plugin
has run, and apply it to all peer-controlled error text, including plugin-name
diagnostics. Preserve error identity, numeric server code and SQLSTATE. Cover
initial-plugin and switched-plugin paths, known and unknown switches, and errors
after the switch; do not remove legitimate auth-switch support to obtain secrecy.

## P2: bound native work until its actual cleanup finishes

[`newClient`](../gssapi/provider.go#L64) starts a worker for every caller. Context
cancellation returns promptly and signals Close, but an already-running native
open or step finishes later under the OS retry policy. That ownership design
correctly avoids races with native cleanup; it does not impose any admission
limit on the work that outlives callers.

The [public comment](../gssapi/provider.go#L46) recommends bounding connection
attempts with `database/sql.MaxOpenConns`. A failed or canceled Connect releases
its pool slot while its native worker can still be running. Repeated short-lived
connection attempts can thus accumulate native workers, OS threads during C calls,
credential activity and memory even with a pool limit of one. A stalled KDC or
credential backend makes this a practical reliability and resource-usage problem.

The [race-enabled probe](../validation/results/stage5-review-20261112/native-budget_test.go.txt)
injects a held native open, cancels each caller and waits for its return before
starting the next. It observes **eight outstanding opens after eight sequential
callers**. Releasing the fake native operation then completes cleanup exactly
once for every state: this demonstrates unbounded outstanding work, rather than
claiming a permanent native-resource leak. See the
[failure log](../validation/results/stage5-review-20261112/native-budget.log).
No real KDC call, performance workload or SQL server was needed for this probe.

**Correction:** add context-aware admission before starting native work, and keep
the capacity reserved until the owning worker has actually finished cleanup.
Account for canceled opens and blocked steps, not just calls still visible to
the SQL pool. Document the scope and capacity of the bound; a caller whose wait
is canceled must not create another worker. Test saturation, cancellation while
waiting, later capacity recovery and independent contexts without serializing
every healthy authentication behind one global worker.

## P2: require provenance for current external-authentication captures

[`external_auth_evidence.validate`](../validation/external_auth_evidence.py#L16)
returns successfully when both `external_auth` metadata and the raw
`EXTERNAL_AUTH` event are absent. Its historical/synthetic exemption is not
restricted to historical captures, so it also applies to a successful capture
of the current implementation and its newly required external-auth tests.

The [saved reproduction](../validation/results/stage5-review-20261112/provenance-probe.py.txt)
copies the final successful Mac receipt and log into temporary storage. It removes
the metadata and its one raw output event, preserves every test identity/status,
command, source input and server/prior snapshot, and reconciles the changed log
checksum and parsed output. The **real `correctness.validate` accepts it**. No
validator function is mocked and no receipt is published. The
[result](../validation/results/stage5-review-20261112/provenance-omission.json)
also checks that the official files and source inputs remain unchanged.

Consequently the collector/verification contract can lose the fixture image,
source, realm, server and certificate attribution while still accepting all
passing test statuses. This is a missing completeness check; it does not imply
the actual final captures lack their recorded provenance.

**Correction:** require the external-auth contract for captures using the current
test/schema contract. Keep historical validation explicit and constrained, rather
than sharing an absence exemption with new production captures. Exercise omission
of both fields through the actual correctness and full-run publication paths;
synthetic unit fixtures must not weaken the production acceptance rules.

## Checks and limits

The [existing focused authentication tests](../validation/results/stage5-review-20261112/existing-auth-tests.log)
pass with `-race -count=1`, including core protocol, permission/downgrade,
callback/panic cleanup and provider cancellation/open-failure checks. All
[75 Python harness tests](../validation/results/stage5-review-20261112/harness-tests.log)
pass. [Read-only freshness verification](../validation/results/stage5-review-20261112/freshness.log)
passes for the saved source and immutable references. The new review-only Go
tests are appended through overlays: production and permanent test files were
not changed. Their expected failures and the provenance acceptance expose cases
outside the existing green coverage.

The prior [complete Mac/Linux confirmation](../validation/results/stage5-confirmation.json)
still records 1,377/1,380 passing leaves, zero failures and 8/5 skips for that
coverage. This review does not repeat the complete real-service suite or add a
Linux execution claim for the new probes. Native-worker accumulation is confirmed
with deterministic held native calls, not a measured production-KDC incident.
No performance improvement or regression is inferred without measurement.

The [review receipt](../validation/results/stage5-review-20261112/review.json)
records the commands, expected outcomes and artifact hashes. Recommended next
work is to fix these three findings with permanent regressions, then reconfirm
the complete correctness profiles; performance remains deferred.
