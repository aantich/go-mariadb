# Stage 3 code review

12 November 2026. Review of all uncommitted native API, test, reporting and
documentation changes on `master`, parent `b33f66b`. Reviewed executable input
fingerprint: `d40de51ee6e045d07c242a9af50e0356a6cd5dd7e2d362570b9df7b1cc37a2a4`.
This pass reports findings; it does not implement production/harness fixes,
change registered tests, rerun benchmarks, alter references, commit or push.

**Subsequent authorized correction:** All six findings below are addressed by
the [fix record](STAGE3_REVIEW_FIXES.md), with new regression tests and validation
receipts. This document retains the original review and counterexamples.

## Findings

All six findings are P2. No P0/P1 issue was established.

| ID | Finding | Location | Evidence |
| --- | --- | --- | --- |
| R1 | Cursor materialization errors lose MariaDB error identity | [cursor.go](../cursor.go#L73) | Real SELECT error 1242 becomes `malformed packet`; deterministic peer confirms connection retirement. |
| R2 | Stream chunks can hit the server's exclusive packet boundary | [stream.go](../stream.go#L59) | On an isolated 64 KiB server, buffered 65,529-byte input succeeds; streamed input fails with 1153. |
| R3 | A callback's ErrBadConn discards the joined SQL error | [native.go](../native.go#L47), [progress.go](../progress.go#L67) | Deterministic progress callback + SQL error 1062 returns only `invalid connection`. |
| R4 | Full benchmark publication bypasses the strict check contract | [record.py](../validation/record.py#L345), [verify.py](../validation/verify.py#L26) | Verifier accepts an actual-receipt counterexample replacing race with `true`; new progress fixture skips escape historical-pass protection. |
| R5 | Full-run server provenance comes from an unrelated admin socket | [record.py](../validation/record.py#L313) | Source confirms the probe ignores configured TCP host/port/account/schema, and has no post-run snapshot. |
| R6 | New bulk/stream tests do not verify rollback effects | [stage3_test.go](../validation/stage3_test.go#L58), [stage3_test.go](../validation/stage3_test.go#L126) | Changing both ROLLBACK commands to COMMIT still passes all fourteen live transport cases with race detection. |

### R1 — Preserve terminal server errors after cursor metadata

`queryCursor` sends the packet following column definitions straight to
`terminatorStatus`. MariaDB can legally send ERR there if cursor materialization
fails. That packet is rejected as malformed, losing its number/SQLSTATE and
retiring an otherwise synchronized connection.

Live reproduction on MariaDB 12.3.3:

```sql
SELECT t.seq,
       (SELECT q.seq FROM seq_1_to_2 q WHERE q.seq <= t.seq) AS v
FROM seq_1_to_3 t;
```

Ordinary queries return error **1242 / 21000**, after the first row. Cursor mode
returns **malformed packet**, with no `MySQLError`. Detect/decode ERR before the
cursor-open terminator, preserve SQL error identity and distinguish a complete
server error from a damaged transport. Add live and peer regressions, including
legacy EOF and compressed transport.

### R2 — Keep a spare byte at the packet limit

The stream clamp uses `maxAllowedPacket - 7`, so a full chunk plus LONG_DATA's
seven-byte command body reaches exactly the configured maximum. MariaDB's
`net_realloc` rejects `length >= max_packet_size`. The existing buffered upload
path already accounts for this with `maxAllowedPacket - 1 - overhead`.

An isolated MariaDB 12.3.3 server started with `--max-allowed-packet=64K`, and a
client explicitly configured with `MaxAllowedPacket=65536`, reproduces this:

```text
stream=false bytes=65529 sql_error=0 error=<nil>
stream=true  bytes=65529 sql_error=1153
```

The value itself is below the complete-parameter limit. Align stream chunking
with the existing buffered path and test limits below/at the chunk size, including
explicit client maxima and automatic server-limit discovery. The shared Mac's
global settings were not changed; the temporary Linux server was stopped.

### R3 — Classify driver failures before joining application errors

The native wrapper applies `errors.Is(err, driver.ErrBadConn)` to the entire
operation error. Progress execution has already joined the callback error at
that point. If the callback returns that sentinel, directly or wrapped, the
wrapper replaces the whole joined error with `ErrInvalidConn` and retires the
connection. A simultaneous server error is lost, violating the documented
callback/server error-preservation contract.

A peer sends progress then SQL error 1062; a callback returning ErrBadConn causes
the helper to return only `invalid connection`. Sanitize actual driver replay
markers before joining callback errors, and preserve the application/SQL errors
without misclassifying them as transport failures. Test direct/wrapped callback
sentinels on both SQL success and failure.

### R4 — Enforce required checks on the full measurement path

The new REQUIRED fixture/fault policy is enforced by `ci.py`, and strict command/
raw-log contracts by `correctness.py`. The full `record.py` path instead trusts
process exits plus historical reference passes. New Stage 3 API tests are absent
from those old references, so the historical guard does not require their
presence or reject their skips. Its environment also inherits GOFLAGS/GOWORK.

For matching full-capture inputs, `verify.py` checks benchmark completeness and
references but does not verify check commands, required test events or raw-log
consistency. In a temporary copy of the actual final receipt, replacing its race
stage with `{"command":["true"],"exit_code":0,"cases":[]}` is accepted as fresh.
Changing the new progress fixture's statuses to skip also leaves the historical
pass-loss list empty.

The actual completed captures did execute race/vet/tests; this counterexample
does not retroactively claim those checks were absent. Apply shared required-
coverage and raw-log validation to full captures too. Define their check command
contract explicitly where it differs from correctness-only commands, and clear
or validate compilation-affecting environment options.

### R5 — Probe the server being measured, before and after

The full runner's preflight invokes an administrative client with
`--protocol=socket`, while driver tests/benchmarks use host/port/account/schema
from `--config`. A different configured endpoint can therefore be timed while
the local service's settings are recorded and compared with the reference.
There is also no after-run check for settings drift.

This is source-confirmed. The present local fixture points to the same server;
the review does not establish a wrong-server measurement in the saved captures.
Reuse the existing TCP provenance implementation to probe the actual benchmark
endpoint before/after, retain checksummed snapshots and fail profile comparison
on mismatch. Add a two-endpoint negative control without changing shared-server
global settings.

### R6 — Assert data after ROLLBACK, before later operations

Bulk units tests roll back an UPDATE, then perform further writes and merely
require a later sum to be at least six. Both committed and rolled-back data pass
that check. Stream tests finish at ROLLBACK without checking the inserted row's
absence. They exercise transaction commands but do not establish rollback.

A temporary Go overlay changes only the two ROLLBACK calls to COMMIT; both tests
still pass on every one of their seven transport/metadata modes. Add exact
post-rollback count/value assertions before subsequent writes, and require this
negative control to fail. No production transaction bug is claimed from this
coverage counterexample alone.

## Verification and limits

The [review evidence](../validation/results/stage3-review-20261112/capture.json)
retains commands, logs, source probes and checksums. Temporary Go overlays add
review probes or mutate only test transaction commands; repository Go files
remain unchanged. Probe source is saved as `.txt` so normal recursive package
discovery cannot accidentally run it.

* The cursor failure reproduces on the real shared MariaDB using read-only SQL.
* The stream failure reproduces on isolated Linux with a 64 KiB server packet
  limit; ordinary buffered binding is its successful control.
* Error-identity probes run with race detection and fail the expected contract
  assertions; their failures are review evidence, not added registered-suite cases.
* The fourteen rollback-negative transport cases incorrectly pass, proving R6.
* Existing native tests pass with race detection; all **46** harness tests pass.
* Current source/reference freshness and `git diff --check` pass. The prior
  complete 1,196/1,199 Mac/Linux results and timing captures remain unchanged.

An exploratory probe also observed retained cursor owner/statement pointers
after Close. It is **not promoted to a leak finding**: database/sql.Rows itself
retains its driver connection, so an incremental public-API memory leak was not
established. This avoids treating a pointer-cleanup preference as a defect.

The recorded performance signals and the missing dedicated throughput baselines
for the four new helpers remain the previously documented work in
[the benchmark follow-up](STAGE3_BENCHMARKS.md). No performance measurements were
repeated in this review. Fixes remain for a subsequent authorized implementation.
