# MariaDB application integrations

Readiness remains **Alpha**. These are tested application patterns, not evidence
from production users. The [application report](../validation/APPLICATIONS.md)
records exact compilers, servers and results. Go 1.25 or newer is required.

## Install from this checkout

No independent version has been published. In your application's module, set
the core replacement to your actual absolute checkout path:

```sh
go mod edit -require=github.com/aantich/go-mariadb@v0.0.0
go mod edit -replace=github.com/aantich/go-mariadb=/absolute/path/to/go-mariadb
```

The executable [core consumer](../validation/apps/consumer/main.go) is also built
from a private source export with workspaces, CGO and network downloads disabled
in every application profile. This checks source packaging, not availability on
the Go module proxy. A future public release will need actual version tags.

## GORM

The optional adapter uses GORM's shared MariaDB-capable SQL generator and this
fork's dedicated transport. Install the tested framework versions and the local
adapter replacement in the consuming module:

```sh
go mod edit -require=github.com/aantich/go-mariadb/integrations/gorm@v0.0.0
go mod edit -replace=github.com/aantich/go-mariadb/integrations/gorm=/absolute/path/to/go-mariadb/integrations/gorm
go get gorm.io/gorm@v1.31.2 gorm.io/driver/mysql@v1.6.0
go mod tidy
```

Both replacements are necessary: Go ignores replacement directives in dependency
modules. Keep the root module and adapter at matching source revisions.

```go
import (
    "errors"
    mariadb "github.com/aantich/go-mariadb"
    mariadbgorm "github.com/aantich/go-mariadb/integrations/gorm"
    "gorm.io/gorm"
)

g, err := gorm.Open(mariadbgorm.New(mariadbgorm.Config{Conn: db}), &gorm.Config{
    PrepareStmt: true,
    TranslateError: true,
})
if err != nil { return err }
// db is an existing *sql.DB built with mariadb.NewConnector or sql.Open("mariadb", ...).
err = g.WithContext(ctx).Create(&record).Error
if errors.Is(err, gorm.ErrDuplicatedKey) {
    // Apply the application's duplicate-key policy.
}
var server *mariadb.MySQLError
if errors.As(err, &server) {
    // The original MariaDB number, SQLSTATE and message are still available.
}
```

Alternatively use `mariadbgorm.Open(dsn)` for the ordinary DSN path. Configure
native functional options through `NewConnector` and an existing pool: those
options are not serialized by `FormatDSN`. The caller owns that pool. Close
GORM's prepared statement cache before closing it at shutdown:

```go
if cache, ok := g.ConnPool.(*gorm.PreparedStmtDB); ok {
    cache.Close()
}
```

When enabled, translation adds `gorm.ErrDuplicatedKey` for MariaDB 1062 and
`gorm.ErrForeignKeyViolated` for 1451/1452 using `errors.Join`. Use `errors.Is`,
not direct equality. `errors.As` still finds the original `*mariadb.MySQLError`.
Other errors pass through unchanged; disabling `TranslateError` retains ordinary
driver errors. The shared generator's original translator recognizes only the
upstream driver's concrete type, which is why this adapter is needed.

`Conn` must directly use this driver's `MariaDBDriver`; a pool belonging to the
upstream transport is rejected before querying it. Driver wrappers which replace
that concrete driver identity need their own integration. The optional module
transitively includes `go-sql-driver/mysql` through GORM's SQL generator, but
selects `mariadb` for all connections. This does not add MySQL server support.
The core driver's dependencies remain unchanged.

The tests exercise repeatable `AutoMigrate`, adding a column to existing data,
batch insertion, upsert, deletes, foreign keys, rollback and statement-cache
recovery. Production migrations still need an explicit deployment strategy:
MariaDB DDL can commit implicitly and an arbitrary schema change is not atomic
because it is called from a GORM transaction.

## sqlx

Install `github.com/jmoiron/sqlx@v1.4.0`. sqlx needs the placeholder style
associated with the dedicated registration:

```go
import "github.com/jmoiron/sqlx"

sqlx.BindDriver("mariadb", sqlx.QUESTION)
x := sqlx.NewDb(db, "mariadb") // wraps the same pool; do not close it twice
_, err := x.NamedExecContext(ctx,
    "INSERT INTO people(id,name) VALUES(:id,:name)",
    []map[string]any{{"id": 1, "name": "Anton"}, {"id": 2, "name": "Anna"}})
if err != nil { return err }
```

Include the explicit column list for sqlx's batch tuple expansion. Named
statements, `sqlx.In` followed by `x.Rebind`, `BeginTxx`, struct scanning and
MariaDB `INSERT ... RETURNING` are exercised by the suite. sqlx returns ordinary
driver errors, so use `errors.As` with `*mariadb.MySQLError`.

Use strings or a decimal type with `Scanner`/`Valuer` for exact DECIMAL values;
`float64` cannot preserve arbitrary precision. Use `uint64` for unsigned BIGINT,
nullable destinations for SQL NULL, and `ParseTime` plus explicit UTC location
and SQL `time_zone` for the tested microsecond DATETIME behavior. MariaDB JSON
is textual; this suite verifies the tested JSON values rather than MySQL's
binary JSON protocol. BLOB bytes must be copied if retained from `sql.RawBytes`.

## Transactions and uncertain outcomes

The [tested order/outbox function](../validation/apps/example/order.go) stores
both records in one InnoDB transaction with a unique application operation ID.
Its [tests](../validation/apps/examples_test.go) check success, duplicates and
rollback when the second write fails. Create its schema explicitly:

```sql
CREATE TABLE go_app_example_orders (
  operation_id VARCHAR(100) PRIMARY KEY, payload BLOB
) ENGINE=InnoDB;
CREATE TABLE go_app_example_outbox (
  operation_id VARCHAR(100) PRIMARY KEY, payload BLOB
) ENGINE=InnoDB;
```

Copy the example into your application and adapt the table names. It lives in
the validation module, not the public runtime API. Allocate a stable operation
ID before attempting the transaction. If COMMIT fails after a disconnect,
reconcile that ID using a fresh connection before deciding to retry. A transport
error does not prove the transaction was rolled back. This is not an exactly-once
delivery guarantee; the outbox publisher also needs its own delivery policy.

## Migrating from go-sql-driver/mysql

1. Change imports to `mariadb "github.com/aantich/go-mariadb"` and SQL driver
   registration to `mariadb`. Update connector/config/error type references.
2. Keep existing DSNs only after reviewing their options in the [user guide](USER_GUIDE.md).
   Connect to MariaDB; MySQL and TiDB are outside this fork's support scope.
3. Use the GORM adapter above or initialize sqlx's dedicated binding explicitly.
   Audit direct error equality/type assertions; translated GORM errors are joined.
4. Keep existing transaction boundaries. Native bulk, session reset, cursors,
   streaming and CDC are explicit APIs/options; importing the fork does not
   silently enable them. Review resource ownership before enabling each one.
5. Run your application's migrations and workload against its actual MariaDB,
   authentication, network/proxy and failure-recovery configuration before rollout.

Close Rows, check iteration errors, use bounded request contexts and size pools
for the application. Cancellation of an active command retires its connection;
the next request can use a fresh one. Opt-in managed reset clears session state:
code depending on session-local variables must pin a connection or restore
the required state through configured initialization. Neither pool reuse nor
framework retry logic makes a partially executed write safe to repeat.

The upstream [GORM connection documentation](https://gorm.io/docs/connecting_to_the_database.html)
describes custom driver names and existing pools. This project's adapter adds
dedicated transport checks and MariaDB error translation to those mechanisms;
its exact dependency versions are recorded in the application module and receipts.
