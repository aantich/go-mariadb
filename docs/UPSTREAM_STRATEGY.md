# Upstream contributions or a separate MariaDB driver?

**Decision, later on 11 November 2026:** Anton chose a dedicated MariaDB driver
and declined upstream PRs. The recommendation below is preserved as historical
analysis. See [the implementation record](DEDICATED_DRIVER.md) for current work.

Assessment: **11 November 2026**. This is a recommendation before implementation,
not an approved package or API design. Production code, inherited tests and the
[original baseline](../validation/RESULTS.md) remain unchanged. No upstream PR,
issue or discussion has been submitted on Anton's behalf.

## Recommendation

**Contribute fixes and focused performance improvements upstream first. Keep this
fork as the development and measurement checkout while testing whether upstream
will accept the native bulk API. Decide on a separate public driver after that
design decision.**

Upstream is a viable home for substantial MariaDB work. It already implements
MariaDB capability negotiation and metadata omission. Several of our findings
already have open PRs. A new backend framework is unnecessary for these changes.

There is also a real scope constraint: upstream supports MySQL and MariaDB, and
its maintainers evaluate MariaDB-only public features individually. Our complete
roadmap cannot be assumed acceptable. A MariaDB-only release remains a reasonable
choice if native bulk and the subsequent APIs cannot be supported upstream on
an acceptable schedule. Maintaining our own driver gives release control but
also makes us responsible for ongoing protocol, security, Go and database/sql
compatibility work.

## What the architecture actually allows

Source inspection is pinned to upstream/fork commit
`e6232456bb6eb92a117563823ac6956937410b96`.

| Mechanism | Existing behavior | Consequence |
| --- | --- | --- |
| Driver and connection creation | One `MySQLDriver`, private `connector`, private `mysqlConn`; no MySQL/MariaDB backend factory | There is no drop-in MariaDB protocol backend to implement externally. |
| Capability negotiation | The greeting supplies ordinary and MariaDB extended capabilities; `initCapabilities` intersects supported flags and enables metadata caching | Native features can be added to the existing implementation and selected by negotiated capability. A version string alone is insufficient. |
| Authentication | Public per-exchange `AuthPlugin` / `AuthContext` and `RegisterAuthPlugin` on master | Optional authentication packages are possible, subject to the challenge-data correction below and a compatible upstream release. |
| Configuration and transport | `Config.Apply`, functional options, `BeforeConnect`, `DialFunc` and dial registration | Useful extension points for policy and transports; none replaces command encoding, result parsing or connection ownership. |
| Compression | The optional `zstd` subpackage uses a purpose-built internal codec hook | Optional dependencies are possible when core provides the hook. This internal hook is not a general external protocol extension API. |
| `sql.Conn.Raw` | Temporarily exposes the underlying driver connection inside a callback | A small exported capability interface could expose native operations. Current private packet/statement state prevents a normal wrapper from implementing bulk, cursor fetch or reset itself. |

Primary source anchors:
[driver registration and connector API](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/driver.go#L81),
[connection construction](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/connector.go#L64),
[capability selection](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/packets.go#L274),
[authentication interface](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/auth_plugin.go#L16),
[Go's Raw lifetime contract](https://pkg.go.dev/database/sql#Conn.Raw).

Writing extra command bytes through a wrapped `net.Conn` would bypass the
driver's TLS/compression, packet sequencing, buffers, cancellation and pending
results. `Raw` does not make those internal operations public or permit retaining
the connection after its callback. A companion package can orchestrate SQL or
implement auth plugins; it cannot safely supply all our missing wire APIs today.

**Release distinction:** GitHub's latest published upstream release is
[v1.10.1, 2 September 2026](https://github.com/go-sql-driver/mysql/releases/tag/v1.10.1).
The auth interface merged on 5 October and is absent from that tag. Our baseline
uses development master, not v1.10.1. An auth companion would initially need a
pinned development version or a later stable release containing that interface.
The baseline also must not be presented as a comparison against the latest tag.

## Evidence about upstream acceptance

* [PR #1708](https://github.com/go-sql-driver/mysql/pull/1708), MariaDB metadata
  skipping and EOF changes, **merged 26 April 2025**. This is concrete evidence
  that MariaDB-specific performance extensions can belong in the shared driver.
  Earlier PR #1692 was closed in favor of #1708; it was not the merged PR.
* [PR #1696](https://github.com/go-sql-driver/mysql/pull/1696), authentication
  extensibility, **merged 5 October 2026**. Its stated motivation included
  MariaDB auth plugins. Maintainer discussion required a narrow public interface
  rather than exposing private `mysqlConn` internals.
* In that discussion, a maintainer explicitly said they would consider a PR for
  [prepare/execute pipelining #1689](https://github.com/go-sql-driver/mysql/issues/1689)
  because of the round-trip cost. They asked for other feature proposals in the
  [Ideas forum](https://github.com/go-sql-driver/mysql/discussions/categories/ideas)
  and emphasized limiting public features to demonstrated demand.
  [Maintainer comment](https://github.com/go-sql-driver/mysql/pull/1696#issuecomment-4333792093).
* API review and release timing are material uncertainties. #1696 took roughly
  18 months from opening to merge; its maintainer explained the long-term public
  API concern. The explicit reset PR below remains open. These observations do
  not establish that a new focused PR will be delayed or rejected.

We should bring equivalent-workload measurements and a small, durable API to
that discussion. Removing MySQL compatibility is incompatible with an upstream
contribution; upstream patches must preserve its supported server matrix.

## Existing work overlaps our findings

States below were checked through GitHub's API on 11 November 2026. PR descriptions
are author claims, not proof that the proposed fixes pass our suite. None of these
branches has been integrated or run against our frozen baseline during this
assessment.

| Our area | Existing upstream work | Proposed handling |
| --- | --- | --- |
| Truncated initial greeting | [#1850](https://github.com/go-sql-driver/mysql/pull/1850), open | Test/review the existing patch, including MariaDB's extended capability word; avoid a duplicate PR. |
| Truncated OK packets | [#1815](https://github.com/go-sql-driver/mysql/pull/1815), open | Exercise the existing patch with all our length-encoded/truncation cases; contribute remaining cases where useful. |
| Short prepare responses | [#1831](https://github.com/go-sql-driver/mysql/pull/1831), open | Validate both malformed success and valid ERR responses before proposing additional guards. |
| Raw binary auth challenge | [#1825](https://github.com/go-sql-driver/mysql/pull/1825), open | Review before depending on external PARSEC/ed25519 plugins. Add a separate deterministic regression in future implementation work. |
| PARSEC | [#1827](https://github.com/go-sql-driver/mysql/pull/1827), open; depends on the auth changes | Validate against our real account and malformed/resource-limit cases; preserve authorship and coordinate rather than reimplementing independently. |
| Explicit reset | [#1734](https://github.com/go-sql-driver/mysql/pull/1734), open | Assess lifecycle/restoration semantics against our desired opt-in policy. This proposal alone does not establish a complete clean-pool contract. |
| Prepared metadata API | [#1743](https://github.com/go-sql-driver/mysql/pull/1743), open | Review API overlap first. Exposing ordinary prepare metadata is distinct from negotiating MariaDB extended type metadata. |
| DDL/reprepare | [#1740](https://github.com/go-sql-driver/mysql/pull/1740), open | Check missing error/DDL scenarios separately; ordinary MariaDB DDL reprepare already passes our live tests. |

The auth issue is also **confirmed by static source inspection**: current
[`parseAuthSwitchData`](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/auth.go#L232)
removes a final zero byte from every challenge. The built-in ed25519 plugin
requires exactly 32 bytes, so a valid 32-byte challenge ending in zero is shortened
and rejected. Our captured successful ed25519 connections do not cover that
deterministic edge case. It is newly identified coverage, not a retrospectively
added baseline failure or a newly run live test.

Searches for `COM_STMT_BULK_EXECUTE` and bulk-related PRs found no matching native
bulk implementation proposal. This is a bounded search result, not proof that
none exists elsewhere. Recheck before opening anything.

## Where to place the remaining work

| Work | Suggested destination | Main condition |
| --- | --- | --- |
| Parser bounds, missing/mismatched metadata cache, protocol failure retirement | Upstream core | Small regression-driven patches, preserving valid fast paths and cancellation/error identity. |
| Receive/auth/metadata/decompression budgets | Upstream core proposal | Agree option/default semantics first; do not silently change outbound `maxAllowedPacket` into a new incompatible contract. |
| Unsigned result overflow | Upstream core; design discussion for unsigned extension | `database/sql.Result` is signed. Existing `mysql.Result` also exposes signed slices without errors; fixing its overflow behavior requires an explicit compatibility decision. |
| Prepare/execute pipelining | Upstream core performance PR | Already requested, potential round-trip win through existing application APIs. Drain both responses on errors and preserve no-unsafe-replay behavior. |
| Parameter type reuse, bounded long-data copies and compression-buffer retention | Upstream core | Prove gains and bounds with equivalent workloads; preserve type/signature invalidation and error recovery. |
| Native bulk/per-unit results | Upstream core plus a small opt-in extension API; fork if declined | Agree input ownership, SQL connection lifetime, partial-result semantics, errors and capability discovery before a large implementation. |
| Opt-in reset, session tracking, extended types, readers and cursors | Core hooks plus optional helpers where appropriate | Each needs a stable ownership/cancellation contract. Domain conversions can live outside core; wire decoding and statement generations cannot. |
| PARSEC/PAM/GSSAPI | Existing upstream PR or optional auth packages | Correct raw challenges first; verify transport policy and real service fixtures. Keep optional OS/C dependencies out of the core by default. |
| CDC/GTID/binlog | Separate optional package | Distinct stream/checkpoint API and lifecycle; does not require replacing the SQL driver. |

Unsigned results need care beyond a cast: [issue #738](https://github.com/go-sql-driver/mysql/issues/738)
was closed after noting the signed standard interface. Our proposed correctness
behavior is an overflow error plus an additive unsigned API; acceptance of that
behavior is unconfirmed. Existing signed multi-result accessors cannot acquire an
error return without breaking callers.

Likewise, [reset issue #1273](https://github.com/go-sql-driver/mysql/issues/1273)
records objections to paying for reset on every ordinary pool reuse. Keep reset
explicit or opt-in and benchmark whole equivalent request boundaries. A raw
reset command invalidates prepared state; it is not interchangeable with a fully
specified reset-and-restore policy.

## Proposed next sequence and decision gate

1. Keep the frozen baseline and current module identity. Map existing PR patches
   against our deterministic and live assertions in isolated comparison inputs;
   do not modify baseline workloads or attribute others' fixes to us.
2. Prepare focused upstream-ready fixes/tests for uncovered parser/cache/result
   defects. Split generic hardening from new policy defaults and public APIs.
   Follow [CONTRIBUTING](https://github.com/go-sql-driver/mysql/blob/e6232456bb6eb92a117563823ac6956937410b96/.github/CONTRIBUTING.md):
   check existing work, add regressions, preserve notices and add AUTHORS entries.
3. Make prepare/execute pipelining the first upstream performance candidate.
   Measure local and controlled-latency profiles separately; no speed ratio is
   established for the unimplemented path.
4. Draft the native bulk API with representative call sites, protocol behavior,
   failure/lifetime rules and equivalent benchmark design. Seek an upstream
   design decision before investing in the full public extension surface.
   Posting that discussion or any PR requires Anton's separate instruction.
5. **Choose distribution:** use upstream if it accepts the required core hooks
   and a practical release path; use companion modules for functionality supported
   by those hooks. Publish a dedicated MariaDB driver if essential APIs are
   declined, require rejected core hooks, or upstream timing cannot meet an
   agreed project milestone. Absence of an immediate reply is not a rejection.

If a dedicated driver is chosen, assign its own module path and `mariadb` SQL
registration name before release. Importing two packages that both register
`mysql` can panic; changing imports alone does not resolve that collision.
Retain attribution and license notices, track upstream fixes continuously, and
maintain the MariaDB/Go/platform matrix. Use an explicit comparison adapter for
module-identity changes while preserving the original benchmark bodies/results.

A fork's performance advantage must come from measured protocol and implementation
changes. A backend interface or a MariaDB-only name creates no speedup by itself.
Select capabilities once per connection or operation where possible, keep extra
work off the row-decoding path, and preserve existing metadata caching and Go 1.27
scanning. Upstream validation additionally needs MySQL controls; MariaDB-only CI
removal in the original roadmap applies only if we choose an independent driver.
