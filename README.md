# Go-MariaDB

A dedicated, pure-Go **MariaDB** driver for `database/sql`, forked from
[go-sql-driver/mysql](https://github.com/go-sql-driver/mysql).

**Alpha:** automated tests cover MariaDB 12.3.3 locally. Application pilots,
wider server/platform validation and several native APIs remain outstanding.
This is a development checkout; no independent release has been published.
MySQL and TiDB are outside this project's support scope.

**[User guide and examples](docs/USER_GUIDE.md)** ·
**[Live correctness and benchmarks](validation/RESULTS.md)** ·
**[Implementation record](docs/DEDICATED_DRIVER.md)** ·
**[Review corrections](docs/STAGE1_REVIEW_FIXES.md)** ·
**[Session foundations](docs/STAGE2_SESSION_FOUNDATIONS.md)** ·
**[Session review corrections](docs/STAGE2_REVIEW_FIXES.md)** ·
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
* MariaDB metadata caching, binary value decoding, Go 1.27 direct column scanning,
  utf8mb4, sockets, verified TLS and client certificates.
* Native binary batch DML through opt-in `BulkExec`, including unsigned values,
  NULL, DEFAULT and IGNORE indicators. Batches are validated and bounded before
  execution. Transaction boundaries remain explicit.
* Native/password, ed25519 and **PARSEC** authentication; authentication work and
  message sizes are bounded. Binary MariaDB challenges retain trailing zero bytes.
* Checked metadata/OK/greeting/prepare parsing, receive and column limits, safe
  retirement after malformed protocol, and unsigned result accessors with signed
  overflow errors.
* LOCAL INFILE file allowlisting/reader handlers, optional zlib and buffered
  long-data parameters with bounded staging storage.
* Opt-in clean session reset/restoration with lazy prepared-handle reprepare;
  bounded session observations and native column attributes through
  `InspectSession`, `Describe` and standard `ColumnTypes`.

Metadata caching, ed25519 and direct scanning are inherited capabilities. Native
bulk, PARSEC and the documented hardening are additions in this fork. Shared wire
names such as `mysql_native_password` remain because MariaDB uses them.

## Performance evidence

The [live report](validation/RESULTS.md) compares the latest complete working-tree
capture against an immutable, unchanged-upstream baseline: the same workloads,
seven repetitions per profile, source hashes and explicit host conditions.
Negative elapsed-time changes mean faster execution. New feature benchmarks have
separate first measurements; they are not old-versus-new driver improvements.
Session foundations have correctness-only validation. The report labels the
earlier timing source separately; no fresh performance run was authorized.

The inherited baseline already showed substantial differences between existing
API alternatives: a prepared 1,000-row/32-integer read took about 0.496 ms versus
2.715 ms for text, and a transactional 100-row text batch took about 0.444 ms
versus 3.607 ms for sequential prepared writes. These are **inherited API-path
comparisons**, not improvements made by this fork, and the original host had
observed background VM activity. See the report for current results and limits.

## Scope and provenance

The current live profile is MariaDB 12.3.3, macOS arm64 and Go 1.27.4. The intended
server range is MariaDB 10.11 and newer; broader CI is not equivalent to completed
runtime validation. Per-unit bulk results, reader parameters, cursors, progress and
external PAM/GSSAPI fixtures remain on the roadmap. CDC is optional future scope.

The fork preserves the upstream MPL-2.0 license and attribution. Internal names
and migration aliases may still contain MySQL. The
[archived upstream README](docs/UPSTREAM_README.md) is historical documentation;
its server support, installation and release claims describe upstream.
