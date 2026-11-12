# Dedicated MariaDB driver: first implementation stage

**Session-foundations follow-up:** The new uncommitted [session stage](STAGE2_SESSION_FOUNDATIONS.md) implements opt-in clean reuse, session observations, native metadata and isolated instrumentation. Its latest correctness source is separated from historical timing evidence in the live report.

**Publication update, 12 November 2026:** Anton approved committing and pushing
the completed stage and review corrections to the fork's existing `master`
branch. Historical uncommitted status below describes the earlier checkpoints.
No package release or additional performance run is part of this delivery.

**Review follow-up, 12 November 2026:** All eight findings in the first-stage
review are corrected, with persistent regressions, fixture-backed CI and
regenerable reports. The [correction receipt](STAGE1_REVIEW_FIXES.md) records
1,018 unique Mac passing leaves, the known clean-session gap, seven skips, exact-source Linux
checks and 200 seven-repetition profiles. Historical implementation/measurement
details below remain unchanged; the linked live report is the latest capture.
The correction receipt explains the historical parser's eleven container labels
and links the later correctness-only confirmation; no further timing runs occur
while Anton is using the Mac.

11 November 2026. Anton chose an independent MariaDB driver instead of upstream
PRs. This supersedes the distribution recommendation in
[UPSTREAM_STRATEGY.md](UPSTREAM_STRATEGY.md). Changes remain uncommitted for
review. There is no release/tag, upstream submission or package publication.

## Implemented

* Module `github.com/aantich/go-mariadb`, Go package `mariadb`, SQL registration
  `mariadb`, client attribution `Go-MariaDB-Driver`. The `MariaDBDriver` type is
  canonical; `MySQLDriver` remains a deprecated source-migration alias. Upstream
  license notices and attribution remain intact. MariaDB-only CI server targets
  replace upstream's MySQL server jobs.
* Checked greetings, length-encoded values, OK counters, column definitions,
  result headers and prepare responses. Supported modern MariaDB greetings must
  include the extended portion; a lower-word-only legacy greeting is rejected.
  Malformed metadata/header/OK/ERR structures retire the connection without
  requesting automatic replay of a potentially executed command.
* Incoming logical-message, authentication, column-count and aggregate metadata
  byte budgets. Metadata/auth body sizes are checked before reads. Defaults
  preserve valid 65535-column prepared results. Ordinary row reads do not test
  mutable authentication-phase state.
* Compression output is bounded to the advertised size. The accumulation bound
  permits a frame containing the end of one wire packet and the start of another;
  a one-frame-only bound incorrectly rejected inherited large-BLOB tests and was
  corrected before final measurement. Long-data staging is capped at 64 KiB;
  the entire buffered parameter is no longer copied to a second full-size buffer.
* A checked one-byte string-length fast path avoids repeating integer-header
  decoding for ordinary scalar columns. All lengths 0..250, truncations and the
  NULL boundary are tested. Longer encodings retain overflow-safe validation.
* Cached-metadata omission validates the declared column count against the
  available statement schema. Exec with omitted metadata no longer tries to read
  nonexistent column packets. Existing reprepare behavior remains covered.
* Signed `LastInsertId`/`RowsAffected` report overflow instead of succeeding with
  negative wrapped values. `UnsignedResult`, `ExecUnsigned` and `BulkExec` expose
  lossless unsigned counters and defensive multi-result copies. Inherited signed
  slice accessors retain their signed-view contract; use the new unsigned API.
  `database/sql`'s ordinary result wrapper does not expose extension interfaces.
* Built-in PARSEC, tested with an actual MariaDB account and independent signature
  verification. Algorithm/salt/nonce lengths and work factor are checked. Ed25519
  and PARSEC binary challenges retain trailing zero bytes. Legacy normalization
  remains for other existing auth plugins; there is no claim that every possible
  external binary plugin is supported automatically.
* Opt-in native binary batch DML, aggregate results, unsigned values, NULL,
  DEFAULT and IGNORE. Every batch is converted/validated/encoded before execute;
  dimensions and payload size are bounded. The helper owns a transient prepared
  handle within `sql.Conn.Raw`, never retains the raw connection, preserves caller
  transaction boundaries and provides no implicit fallback or unsafe write retry.
* Exact bulk payload sizing uses the actual length-prefix width. Short strings
  no longer waste eight buffer bytes each or cause fitting batches to be rejected;
  exact-fit and one-byte-too-small boundaries are tested.
* A [user guide](USER_GUIDE.md), revised public API/resource/lifetime comments,
  Alpha readiness notice, and a documented migration route.

This stage covers the independent package foundation, parser/result hardening,
PARSEC and aggregate bulk. It does **not** implement the whole multi-stage roadmap.
Per-unit results, parameter-type/signature reuse and pipelining remain subsequent
performance work. Session reset/tracking, extended type metadata, reader
parameters, cursors, progress and external PAM/GSSAPI remain subsequent APIs.
CDC is a separate optional scope. These omissions remain visible in the inventory.

## Test and benchmark integrity

The immutable original baseline remains at upstream commit
`e6232456bb6eb92a117563823ac6956937410b96`. It is never relabelled as our fork.
The new recorder fingerprints untracked production files as well as tracked
files; otherwise additions such as `bulk.go` could escape provenance checks.

The original workload bodies remain unchanged. The inverse migration adapter
permits only package/import paths, SQL registration string literals and the
package-qualified type name in one diagnostic expectation. Reversing those exact
metadata substitutions reproduces every original workload SHA-256. Original
Python recorder files and all baseline artifacts are untouched. Test identity
comparison normalizes the module namespace, protecting every original PASS.

Use `validation/record.py` and `validation/verify.py` for current captures. The
historical `run.py`/`check.py` still describe the unchanged-driver capture and are
retained as frozen inputs. New feature workload hashes and first-reference checksums are independently
protected. Interrupted runs checkpoint source fingerprints without publishing a
completed live report. Review stopped partial attempts to correct these guards
and exact bulk sizing; their logs remain retained, and no favorable timing
selection was made. A complete pre-optimization capture and interleaved original/
current narrow-read controls are also retained. The unchanged driver itself
showed substantial drift from its historical timings; this does not prove every
current slowdown is environmental. Performance acceptance remains explicit.

Current source/workload hashes, dirty-tree status,
compiler, modules, settings, commands, raw logs and repeated samples are recorded.

New bulk measurements compare prepared loops, text batches, LOCAL INFILE and
native bulk with identical 1/16/100/1000-row contents, transaction boundaries,
deletion and count/sum/content verification. Input matrices, SQL text batches
and TSV payloads are prebuilt outside timing. Native packing occurs inside the
API call, so this compares reuse of those API input representations, not isolated
protocol speed or the cost of generating text/TSV from application objects. Ordinary prepared controls reuse a
handle; native BulkExec includes transient prepare/close. All sixteen new paths
have an independent first reference; they do not fabricate an upstream native
bulk timing. Performance acceptance for original workloads stays separate.
These new sixteen workloads use Go's benchmark calibration; the original forty
added baseline profiles retain their original explicit warmups where defined.

The full capture and any remaining failures/performance signals are recorded in
[RESULTS.md](../validation/RESULTS.md). Initial development probes found and fixed
too-low column/compression bounds and a package-name diagnostic expectation.
A parser fuzz campaign completed 518,848 executions on the earlier reviewed
source; the final capture repeats fuzzing on its exact inputs.

## Remaining clean-session assertion

The original `TestMariaDBDesiredCleanPoolReset` requests session sanitization
without an opt-in configuration. Ordinary inherited pool checkout is liveness
only. This desired behavior remains a failure and API gap; neither its assertion
nor the baseline is changed to make the totals green. Implement an explicit
reset-and-restore policy next, including statement generations, charset/schema/
role restoration, cancellation and failure retirement. Do not silently reset
every existing pooled request or advertise an incomplete reset command as a full
isolation policy.

## Readiness limits

The live profile remains one Mac, Go 1.27.4 and MariaDB 12.3.3. CI configuration
for other versions is not proof that those jobs have run. No Linux/Windows run,
real application pilot, dedicated WAN experiment or new retained-heap proof is
claimed here. The final benchmark host was not freshly confirmed idle; CPU
observations accompany each process family. Timing regressions require review
and do not automatically advance the original reference.

## Final validation — 12 November 2026 local time

Final source fingerprint `901628d5decc288cc479da04740fd57c5fc7f2a95526b4c0c32d3f89c9c61c1f`:
**958 PASS / 1 desired clean-session FAIL / 7 inherited SKIP**. Inherited/root
checks contribute 668 passes, malformed protocol checks 78, and live MariaDB
checks 212. The race stage repeats 880 passes, the same failure and seven skips;
there are no data-race warnings. Vet, eleven recorder tests and 524,294 fuzz
executions pass. Every original passing case remains protected. Full workflow
exit 1 reflects the known gap, not a successful clean-session implementation.

All 200 profiles have seven retained repetitions. Matched native bulk is about
7.86× faster than prepared loops at 100 rows and 11.82× at 1,000 rows. Text batches
remain faster at 1,000 rows. Prebuilt text/TSV inputs and native transient prepare
costs are explicit. The first bulk reference and original baseline remain intact.

**Performance acceptance remains open:** 18 original profiles exceed +5% elapsed
time, eight exceed +20%, and 17 improve by more than 5%. Narrow compressed reads
and several BLOB paths require investigation. Interleaved unchanged-driver
controls also slow markedly relative to the historical baseline, so neither
universal regression nor universal environmental causation is established.
The checked short-string path removes redundant work; its source-level rationale
is not a claim that it resolved every read-path signal.

[Final raw evidence and analysis](../validation/results/dedicated-stage1/analysis.json),
[earlier complete capture](../validation/results/dedicated-stage1-initial/analysis.json)
and [diagnostic controls](../validation/results/dedicated-stage1-controls/capture.json)
are retained. Current freshness, every frozen-reference checksum and original
workload-body checks pass. No commit, push or release has occurred. The next
functional stage is explicit clean-session reset/restoration; ordinary read-path
performance investigation remains a separate open acceptance item.
