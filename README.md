# Go-MariaDB

A dedicated, pure-Go **MariaDB** driver for `database/sql`, forked from
[go-sql-driver/mysql](https://github.com/go-sql-driver/mysql).

**Alpha:** automated compatibility covers MariaDB 10.11/11.4/11.8/12.3 with Go
1.25/1.26/1.27 in the [recorded profiles](validation/COMPATIBILITY.md).
GORM and sqlx workflows also have [separate application profiles](validation/APPLICATIONS.md).
Production application pilots and wider platform/topology validation remain outstanding.
This is a development checkout; no independent release has been published.
MySQL and TiDB are outside this project's support scope.

**[User guide and examples](docs/USER_GUIDE.md)** ·
**[GORM, sqlx and migration guide](docs/APPLICATIONS.md)** ·
**[Application validation and Alpha readiness](docs/STAGE8_APPLICATIONS.md)** ·
**[Live correctness and benchmarks](validation/RESULTS.md)** ·
**[Implementation record](docs/DEDICATED_DRIVER.md)** ·
**[Review corrections](docs/STAGE1_REVIEW_FIXES.md)** ·
**[Session foundations](docs/STAGE2_SESSION_FOUNDATIONS.md)** ·
**[Session review corrections](docs/STAGE2_REVIEW_FIXES.md)** ·
**[Native bulk, streams, cursors and progress](docs/STAGE3_NATIVE_APIS.md)** ·
**[Stage 3 benchmark follow-up](docs/STAGE3_BENCHMARKS.md)** ·
**[Stage 3 review findings](docs/STAGE3_CODE_REVIEW.md)** ·
**[Stage 3 review fixes](docs/STAGE3_REVIEW_FIXES.md)** ·
**[Reusable native statements and measurement preparation](docs/STAGE4_STATEMENT_REUSE.md)** ·
**[Stage 4 review findings](docs/STAGE4_CODE_REVIEW.md)** ·
**[Stage 4 review corrections](docs/STAGE4_REVIEW_FIXES.md)** ·
**[Optional CDC and durable resume](docs/STAGE6_CDC.md)** ·
**[CDC review corrections and validation](docs/STAGE6_REVIEW_FIXES.md)** ·
**[Version compatibility and failure recovery](docs/STAGE7_COMPATIBILITY.md)** ·
**[Compatibility validation review corrections](docs/STAGE7_REVIEW_FIXES.md)** ·
**[Application integration review corrections](docs/STAGE8_REVIEW_FIXES.md)** ·
**[Roadmap and remaining gaps](docs/ROADMAP.md)** ·
**[Run validation](validation/README.md)**

## Use it

Requires Go 1.25 or later. During development, use a local module replacement:

```sh
go mod edit -require=github.com/aantich/go-mariadb@v0.0.0
go mod edit -replace=github.com/aantich/go-mariadb=/absolute/path/to/go-mariadb
```

```go
import (
    "database/sql"
    _ "github.com/aantich/go-mariadb"
)

// Credentials shown only as placeholders; do not commit real passwords.
db, err := sql.Open("mariadb", "user:password@tcp(127.0.0.1:3306)/app")
if err != nil { /* handle error */ }
defer db.Close()
```

For constructed configuration, import `mariadb "github.com/aantich/go-mariadb"`
and use `mariadb.NewConfig`, `NewConnector` and `sql.OpenDB`. This driver registers
only `mariadb`, so importing the original `mysql` driver alongside it does not
create a registration collision.

## Features

* Standard SQL queries, prepared statements, transactions, context cancellation,
  multi-result statements/procedures and `Scanner`/`Valuer` integration.
* Optional [GORM adapter](integrations/gorm) with MariaDB transport and error
  translation that preserves both GORM and original server error identities;
  [tested sqlx usage](docs/APPLICATIONS.md#sqlx) through the `mariadb` registration.
* MariaDB metadata caching, binary value decoding, Go 1.27 direct column scanning,
  utf8mb4, sockets, verified TLS and client certificates.
* Native binary batch DML through opt-in `BulkExec`, including unsigned values,
  NULL, DEFAULT and IGNORE indicators. Batches are validated and bounded before
  execution. Transaction boundaries remain explicit.
* Owned per-unit bulk counters through `BulkExecUnits`; one-shot `io.Reader`
  parameters through `ExecStream`, with bounded client staging and upload cleanup.
* Explicit `PrepareNative` handles reuse bulk/unit/stream statements safely across
  calls and managed resets, with normal `database/sql` statement ownership.
* Opt-in server cursors using ordinary `sql.Rows`, bounded fetch counts and early
  cleanup; per-command progress callbacks with error/panic draining.
* Native/password, ed25519 and **PARSEC** authentication; authentication work and
  message sizes are bounded. Binary MariaDB challenges retain trailing zero bytes.
* Opt-in interactive PAM with explicit prompt handling and protected transport;
  principal-pinned, mutually authenticated Kerberos through the optional
  [`gssapi`](gssapi) provider on macOS/Linux. See the [authentication guide](docs/USER_GUIDE.md#interactive-pam).
* Checked metadata/OK/greeting/prepare parsing, receive and column limits, safe
  retirement after malformed protocol, and unsigned result accessors with signed
  overflow errors.
* LOCAL INFILE file allowlisting/reader handlers, optional zlib and buffered
  long-data parameters with bounded staging storage.
* Opt-in clean session reset/restoration with lazy prepared-handle reprepare;
  bounded session observations and native column attributes through
  `InspectSession`, `Describe` and standard `ColumnTypes`.
* Optional [`cdc`](cdc) package: a dedicated pull-driven binlog stream, owned
  sparse row images, bounded checksum/compression decoding, complete-group
  checkpoints, MariaDB GTID resume and explicit consumer-controlled semisync ACKs.
  See the [CDC guide](docs/USER_GUIDE.md#change-data-capture-cdc).

Metadata caching, ed25519 and direct scanning are inherited capabilities. Native
bulk, PARSEC and the documented hardening are additions in this fork. Shared wire
names such as `mysql_native_password` remain because MariaDB uses them.

## Performance evidence

The [live report](validation/RESULTS.md) compares the latest complete working-tree
capture against an immutable, unchanged-upstream baseline: the same workloads,
seven repetitions per profile, source hashes and explicit host conditions.
Negative elapsed-time changes mean faster execution. New feature benchmarks have
separate first measurements; they are not old-versus-new driver improvements.
The [Stage 3 benchmark follow-up](docs/STAGE3_BENCHMARKS.md) records the later
authorized measurement of all 200 existing profiles. The original workloads and
references remain fixed; host observations and remaining signals stay explicit.

[Stage 4](docs/STAGE4_STATEMENT_REUSE.md) adds 62 profiles for native reuse,
per-unit bulk, uploads, cursors and progress. **They have not been measured.**
Current correctness is recorded separately; the last 200-profile timing capture
remains unchanged. No throughput gain is claimed for the new reusable handles yet.

The inherited baseline already showed substantial differences between existing
API alternatives: a prepared 1,000-row/32-integer read took about 0.496 ms versus
2.715 ms for text, and a transactional 100-row text batch took about 0.444 ms
versus 3.607 ms for sequential prepared writes. These are **inherited API-path
comparisons**, not improvements made by this fork, and the original host had
observed background VM activity. See the report for current results and limits.

## Scope and provenance

The current live profile is MariaDB 12.3.3, macOS arm64 and Go 1.27.4. The intended
server range is MariaDB 10.11 and newer; broader CI is not equivalent to completed
runtime validation. [Stage 5](docs/STAGE5_AUTHENTICATION.md) adds isolated real
PAM/Kerberos fixtures; Windows SSPI/NTLM and production identity-service integration
remain outside the current provider. [Stage 6](docs/STAGE6_CDC.md) adds optional
CDC with isolated binlog/restart fixtures; XA, two-phase ALTER and engine-specific
replication modes remain explicitly unsupported. Native API limits and ownership
rules are documented in the guide; performance acceptance remains open.

The fork preserves the upstream MPL-2.0 license and attribution. Internal names
and migration aliases may still contain MySQL. The
[archived upstream README](docs/UPSTREAM_README.md) is historical documentation;
its server support, installation and release claims describe upstream.
