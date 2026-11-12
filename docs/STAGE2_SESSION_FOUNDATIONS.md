# Session foundations

**Review follow-up:** The [review corrections](STAGE2_REVIEW_FIXES.md) supersede
the final-source receipt at the end of this original implementation record.
They remove discarded metadata materialization and strengthen correctness
command verification, server provenance and source-export support. Earlier
captures remain unchanged; the live report identifies the latest tested source.

**Publication authorized, 12 November 2026:** Anton requested committing and
pushing the completed stage and its review corrections to `master`. The approved
implementation contract and pre-publication receipts below remain historical;
publication preserves the tested source and does not repeat measurements.

Authorized 12 November 2026, starting at `d7edf02` on `master`. Implement clean
reuse, session observations, native type metadata and isolated instrumentation.
Leave this stage uncommitted. Run correctness checks only, including race/static
checks; do not execute benchmark functions or replace historical timing samples.

The implementation uses these contracts:

1. `WithSessionReset(SessionResetPolicy)` enables clean pool checkout. An explicit
   `ResetSession` helper works on a pinned `sql.Conn`. Restore charset, configured
   role, database, ordered connection parameters and initialization SQL. Initialize
   fresh managed connections identically. Retire a connection on reset or
   restoration failure, preserve cancellation, and never revive a canceled stream.
2. A successful reset invalidates server statement IDs. Existing Go statement
   handles retain their SQL and generation, reprepare lazily and reject changed
   parameter arity before execution. Closing an obsolete handle sends no stale ID.
   Ordinary connections keep liveness-only checkout and incur no reset commands.
3. `WithSessionTracking` negotiates bounded OK/terminator state decoding.
   `InspectSession` returns owned observations on a pinned connection. Observations
   describe what the server reported, not a complete mirror of arbitrary SQL state.
4. `WithExtendedMetadata` negotiates and validates native column attributes.
   Standard `ColumnTypes` exposes native names when available; `Describe` returns
   owned attributes from preparation without executing SQL. Preserve value/scanner
   behavior and metadata caching, including after reset and DDL reprepare.
5. Run isolated Linux MariaDB with performance_schema enabled and require actual
   statement instrumentation there. Keep the shared Homebrew server unchanged.

The desired clean-pool assertion will explicitly opt into managed reuse. Its
original failing baseline, assertion and history remain preserved; an exact
contract migration records only the configuration change. Add independent tests
of ordinary checkout, real two-request isolation/restoration, prepared handle
reuse, role/schema ordering, malformed tracking/metadata, errors, cancellation,
limits and returned-value ownership. Correct current leaf counting without
rewriting the frozen historical runner or its samples.

Publish fresh correctness evidence separately from the prior performance capture.
The live document must identify the latest correctness source and the older timing
source explicitly. Remaining native data APIs, PAM/GSSAPI and CDC stay outside
this stage.

## Implementation and cost

The public options are functional configuration options applied before
`NewConnector`/`sql.OpenDB`; `FormatDSN` does not serialize them. Managed reuse
requires a nonempty configured database and treats an omitted role as NONE.
Initialization is an ordered list of trusted, idempotent SQL statements, not a
callback with reentrant access to a connection while it is being restored.
Fresh managed authentication defers schema selection until after activating the
configured role. A role-only fixture verifies that the account cannot select the
schema without that role, on both fresh connection and reset.

The default path sends no reset/restoration commands. Reprepare state is an
optional per-statement allocation used only by managed connections, and ordinary
prepared execution does not call the reprepare helper. Native attributes use an
optional column extension and native type-name capitalization happens once during
metadata decoding. The shared column/statement structs do gain optional pointer
slots; correctness-only checks do not establish unchanged allocation costs or
throughput. Performance acceptance remains open until authorized measurements.

Automatic clean checkout and explicit cleanup have distinct error boundaries.
Go's pool only honors `ErrBadConn` from `SessionResetter`; otherwise it ignores
that callback error. A failed managed checkout therefore joins the original
error with the pool-discard marker after retiring the stream. No caller command
has been sent at that point. The explicit pinned reset helper returns its
original context/server error directly, without requesting replay. The live
failure test removes an initialization dependency and verifies that subsequent
caller DDL never executes on a partially restored connection.

Snapshots copy raw block bytes and maps, keep unknown block identifiers, enforce
payload/dictionary/count bounds and apply complete parsed updates. They retain
observed facts, including reset generation and response revision, rather than
claiming arbitrary SQL state is fully known. Both negotiated OK terminators and
legacy EOF cleanup are covered, including state in multi-result boundaries.

Extended metadata is supported with and without metadata caching. `Describe`
prepares and closes a transient handle; standard `ColumnTypes` exposes the native
name without changing byte/scalar scanning. Tests cover actual UUID, INET4,
INET6, JSON, VECTOR and geometry columns, repeated cached queries, DDL type
changes and reset. **MariaDB 12.3 sends VECTOR as VARBINARY without a native
attribute**; the catalog still identifies VECTOR and its values round trip.
No value-length heuristic or implicit catalog query invents a different wire type.

## Evidence and reproducibility

The [live report](../validation/RESULTS.md) leads with the latest source's
correctness-only receipt. Its older full-capture failures and timing tables have
an explicit historical heading and their own source fingerprint. Original and
native-bulk references remain immutable. `verify.py` checks fresh correctness,
raw log hashes, retained test events, protected prior passes, original bodies
through exact approved migrations, and complete historical timing/reference
checks independently. It does not claim new timings from a correctness run.

The only live original-test change is configuration of the managed policy in
`TestMariaDBDesiredCleanPoolReset`, recorded in
[the contract migration](../validation/contract-migrations.json). Its assertion
that the next request cannot see a private variable is unchanged. A separate
ordinary-checkout regression protects the original liveness-only behavior.
The former failure is no longer allowlisted in CI. Multi-segment subtest names
now exclude every ancestor from unique-leaf counts; historical counters/logs
remain preserved.

Use a fresh output directory for each attempt:

```sh
python3 validation/prepare_sessions.py
GOCACHE=/private/tmp/go-mariadb-validation-20261111/go-cache python3 validation/correctness.py \
  --output validation/results/new-correctness-attempt --publish
python3 validation/verify.py
```

The shared Mac keeps performance_schema OFF. The disposable Linux fixture starts
with it ON, provisions reserved accounts/role, requires real statement history,
thread identity and connection attributes, and is capped at two CPUs. The
repository mount is read-only. No benchmark or one-iteration smoke is executed.
The image's inherited authentication entrypoint is bypassed explicitly; the
first setup attempt without that override and a missing-Git provenance dependency
were corrected before the isolated suite ran. Source fingerprints remain
available even when Git is absent from the runtime image.

Relevant protocol references, checked 12 November 2026:
[MariaDB reset semantics](https://mariadb.com/docs/server/reference/clientserver-protocol/2-text-protocol/com_reset_connection),
[OK/session-state packets](https://mariadb.com/docs/server/reference/clientserver-protocol/4-server-response-packets/ok_packet),
and [Go's SessionResetter contract](https://pkg.go.dev/database/sql/driver#SessionResetter).
The checked-out server's `sql/protocol.cc`, `sql/session_tracker.cc` and
`sql/sql_type_vector.h` were inspected directly; live behavior is MariaDB 12.3.3,
not a claim that the local server-source checkout was built.

## Remaining scope

Seven API areas remain: per-unit bulk results, streamed prepared parameters,
cursors, progress, interactive PAM, GSSAPI and optional CDC. PAM/Kerberos service
fixtures remain a separate gap. The feature CI job requires instrumented Linux
MariaDB 12.3 on Go 1.25/1.26/1.27; workflow configuration is distinct from completed
hosted matrix execution. This stage remains Alpha and uncommitted for review.

## Final correctness receipt

Final input fingerprint:
`deaa2eb89dd3a961ebd0463b2050b793fb1b1685797cf209a695493b89652e76`.
All **45 production/module inputs** and **60 workload/CI inputs** match between
the Mac and Linux captures and remain stable throughout each run.

| Check | Result |
| --- | --- |
| macOS arm64, Go 1.27.4, Homebrew MariaDB 12.3.3 | **1,063 PASS / 0 FAIL / 8 SKIP**, unique leaves; all tests under the race detector |
| Linux arm64, Go 1.27.4, isolated MariaDB 12.3.3 | **1,066 PASS / 0 FAIL / 5 SKIP**, unique leaves; all tests under the race detector |
| Race warnings | **0** on both profiles |
| Evidence/CI-policy tests | All **35** pass on both profiles |
| Static/workflow checks | vet passes on both; actionlint 1.7.12 passes |
| Protected earlier passes | **0 lost** |
| Performance measurement | **None**, including smoke benchmarks |

The Mac's three additional skips are the inherited connection-attributes and
context-cancel-begin cases plus the new isolated instrumentation profile. The
first and third run with performance_schema enabled on Linux. The common five
skips cover inherited saturation, three deliberately skipped DSN fuzz seeds and
unavailable server zstd negotiation. Exact diagnostics remain in the raw receipts.
These are neither hidden failures nor additional unsupported API implementations.

The [confirmation manifest](../validation/results/stage2-confirmation.json)
links the [Mac capture](../validation/results/stage2-mac-report-final/capture.json)
and [Linux capture](../validation/results/stage2-linux-final/capture.json), raw
logs, source and log hashes, exact skip identities and isolated runtime settings.
The [live report](../validation/RESULTS.md) is regenerated from those correctness
results and the unchanged older timing capture. Older development captures are
retained; earlier fixture/test corrections are not presented as final-source
failures. No timed fuzz campaign or additional toolchain/server matrix run is
claimed. The new fuzz targets' seeds execute as ordinary correctness tests.
