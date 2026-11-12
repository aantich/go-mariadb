# Using Go-MariaDB

The module is `github.com/aantich/go-mariadb`; its package and registered SQL
driver are both **mariadb**. This development version is **Alpha**. The current
tested profile and outstanding failures are in [live results](../validation/RESULTS.md).
Go 1.25 or newer is required. Use a local module replacement until a release
exists; the [README](../README.md#use-it) shows the commands.

## Connect and query

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "os"
    "time"

    mariadb "github.com/aantich/go-mariadb"
)

func main() {
    cfg := mariadb.NewConfig()
    cfg.User, cfg.Passwd = os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")
    cfg.Net, cfg.Addr, cfg.DBName = "tcp", "127.0.0.1:3306", "app"
    cfg.ParseTime = true
    cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 3*time.Second, 10*time.Second, 10*time.Second
    connector, err := mariadb.NewConnector(cfg)
    if err != nil { log.Fatal(err) }
    db := sql.OpenDB(connector)
    defer db.Close()
    db.SetMaxOpenConns(16)
    db.SetMaxIdleConns(8)
    db.SetConnMaxLifetime(3*time.Minute)
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    var version string
    if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil { log.Fatal(err) }
    fmt.Println(version)
}
```

Choose pool size and timeouts for the application. Do not print a configuration
or DSN containing credentials. `sql.Open`/`sql.OpenDB` create a pool lazily; use
`PingContext` to check connectivity before accepting requests.

The DSN format remains `user:password@network(address)/database?option=value`.
Use `Config` when setting functional options; `FormatDSN` does not serialize
native functional options such as `WithBulkOperations` or the receive budgets.

## Parameters, rows and transactions

Pass values as arguments rather than concatenating input into SQL:

```go
rows, err := db.QueryContext(ctx, "SELECT id, name FROM people WHERE id > ?", after)
if err != nil { return err }
defer rows.Close()
for rows.Next() {
    var id uint64
    var name string
    if err := rows.Scan(&id, &name); err != nil { return err }
    // Consume this row.
}
if err := rows.Err(); err != nil { return err }
```

Use `sql.NullString` and other nullable types for nullable columns. Enable
`ParseTime` for DATE/DATETIME/TIMESTAMP values scanned into `time.Time`; `Loc`
controls Go conversion and does not set the server's `time_zone`. For example,
`cfg.Params = map[string]string{"time_zone": "'+00:00'"}` initializes that SQL
session setting. Prepared statements are useful for repeated typed reads/writes;
the inherited binary/scanner paths already outperform text in some workloads.

Use `db.BeginTx`, `defer tx.Rollback()` and `tx.Commit()` for ordinary transactions.
Close rows before reusing their connection. `sql.RawBytes` borrows storage valid
only until the next row/read/close; copy bytes that must outlive that boundary.
Use `NextResultSet` for multiple results and always check `rows.Err()`.

## Verified TLS and authentication

Set `cfg.TLS` to a `tls.Config` with `RootCAs`, the expected `ServerName` and
`MinVersion: tls.VersionTLS12`. Add client certificates when required by the
account. Avoid `InsecureSkipVerify`. A Unix socket uses `cfg.Net = "unix"` and
an explicit socket path; server accounts still control authentication.

PARSEC and ed25519 are registered automatically. Configure the MariaDB account
server-side and supply its password normally. PARSEC validates the binary
challenge, algorithm and salt length and rejects factors above 10 (1,048,576
PBKDF2 rounds). Key material is cleared after signing; the KDF is bounded but
cannot be interrupted halfway through a single derivation. Context cancellation
is checked before and after it. PAM dialogues and GSSAPI are not implemented in
this stage. Protocol names such as `mysql_native_password` remain valid MariaDB
authentication names.

## Native binary bulk DML

Enable the capability before creating the connector:

```go
if err := cfg.Apply(mariadb.WithBulkOperations(true)); err != nil { return err }
connector, err := mariadb.NewConnector(cfg)
if err != nil { return err }
db := sql.OpenDB(connector)
defer db.Close()
conn, err := db.Conn(ctx)
if err != nil { return err }
defer conn.Close()

result, err := mariadb.BulkExec(ctx, conn,
    "INSERT INTO people(id, name) VALUES (?, ?)",
    [][]any{{uint64(1), "Ada"}, {uint64(2), "Grace"}},
)
if err != nil { return err }
fmt.Println(result.RowsAffectedUnsigned())
```

Each batch includes a transient prepare and close. A column must use a consistent
wire type: `int64` and `uint64` are distinct, while NULL/DEFAULT/IGNORE do not
choose the type. Ordinary integer aliases and `driver.Valuer` are converted
before encoding. `nil` means NULL, `mariadb.BulkDefault` requests DEFAULT and
`mariadb.BulkIgnore` uses MariaDB's parameter-ignore semantics where applicable.
Empty input sends no SQL. Unsupported capabilities return
`ErrUnsupportedCapability`; there is no implicit fallback.

Bulk preserves ordinary prepared-parameter date semantics: `time.Time{}` denotes
MariaDB's zero date before timezone conversion; nonzero values use `Loc` and the
configured truncation. Server SQL mode still determines whether zero dates are
accepted. Native bulk is tested with plain TCP, zlib, verified TLS, sockets and
client certificates, including error recovery and cancellation.

BulkExec returns counters and does not expose result sets or RETURNING rows.
A result-bearing response retires the connection rather than leaving unread data
in the pool; use ordinary QueryContext when rows are required. For bulk JSON
parameters, pass `[]byte(rawJSON)` rather than `json.RawMessage` directly.

The entire batch is checked before sending its execute command. Limits are
65535 rows, 1,048,576 cells and `MaxAllowedPacket` payload bytes; oversized input
is rejected rather than silently split. Keep caller data immutable during the
call. Per-unit results and prepared-handle reuse are future APIs. The matched benchmark
prebuilds SQL batches and TSV data, and prebuilds the native/prepared input matrix.
Native packet encoding remains timed. Its results compare these API alternatives;
they do not isolate wire-format costs.

**Bulk execution does not promise transaction atomicity.** For an application
transaction, execute BEGIN/COMMIT/ROLLBACK on the same pinned `sql.Conn`. A server
error may follow partial effects. An uncertain write outcome after disconnect or
cancellation must be resolved by application policy; blindly retrying can duplicate
effects. The driver retires canceled connections rather than attempting reuse.

## Unsigned IDs and counters

Standard `sql.Result.LastInsertId` and `RowsAffected` return `ErrResultOverflow`
when the server value exceeds MaxInt64. Retrieve complete values with the pinned
connection helper:

```go
result, err := mariadb.ExecUnsigned(ctx, conn,
    "INSERT INTO counters(value) VALUES (?)", uint64(1))
if err != nil { return err }
id := result.LastInsertIDUnsigned()
```

`BulkExec` also returns `UnsignedResult`. Its multi-result unsigned accessors
return defensive copies. `database/sql` wraps ordinary `Exec` results, so type
asserting an ordinary `sql.Result` to the extension interface does not work.
The inherited signed `Result.AllLastInsertIds`/`AllRowsAffected` views preserve
signed bit patterns; use the unsigned interface for counters above MaxInt64.

## Limits, compression and session ownership

`WithReceiveLimit(bytes)`, `WithAuthenticationLimit(bytes)` and
`WithMetadataLimit(columns)` configure separate incoming-message, authentication
and column-count budgets. `WithMetadataByteLimit(bytes)` additionally bounds a
whole column-definition block before reading its bodies. Defaults are the
connection's outbound packet maximum, 1 MiB, 65535 columns and 16 MiB of metadata
respectively. The receive override can exceed the outgoing
limit when the application needs large results and small writes. Split logical
messages are counted cumulatively. Compressed frames are bounded before growth,
and zlib output is limited to the announced decoded size. Buffered long-data
uploads use a staging chunk of at most 64 KiB rather than copying the entire BLOB.
This is not yet an `io.Reader` prepared-parameter API.

Enable zlib with `cfg.Apply(mariadb.EnableCompression(true))`. Compression is a
network/CPU tradeoff; use [measured profiles](../validation/RESULTS.md). The optional
zstd package is retained but zstd is not negotiated by the current MariaDB runtime.

Pool reuse currently checks liveness and **does not sanitize SQL session state**.
Keep request-specific variables, temporary tables and role changes on a pinned
connection and restore them explicitly, or close/retire that session according to
application policy. A managed reset-and-restore API remains a documented gap;
the original desired-clean-pool assertion remains failing rather than being
relabelled as success.

## Migration and readiness

Change the module import and SQL registration name from the original driver to
this package. `MySQLDriver` remains a deprecated alias for `MariaDBDriver`, and
some error/internal type names remain inherited. No `mysql` driver is registered
by this package. Preserve upstream license notices when redistributing it.

Consult the [implementation record](DEDICATED_DRIVER.md),
[roadmap](ROADMAP.md) and [coverage limits](../validation/COVERAGE.md).
Current automated checks do not certify production applications, all MariaDB
versions, failover topologies or external authentication services.
