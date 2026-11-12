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
is checked before and after it. Protocol names such as `mysql_native_password` remain valid MariaDB
authentication names.

### Interactive PAM

MariaDB PAM accounts use the `dialog` client plugin. Enable it per connector with
`WithPAMDialog`; it is disabled by default and has no automatic terminal reader.
Use verified TLS as configured above, or a Unix socket. An encrypted connection
with `InsecureSkipVerify=true` is rejected by this policy. The independent legacy
`AllowCleartextPasswords` setting does not enable PAM dialogues.

Using the TLS-configured `cfg` from above, recognize the exact prompts configured
by your administrator and supply each response explicitly:

```go
err := cfg.Apply(mariadb.WithPAMDialog(mariadb.PAMDialogConfig{
    MaxPrompts: 2,
    Prompt: func(ctx context.Context, p mariadb.PAMPrompt) (string, error) {
        if err := ctx.Err(); err != nil {
            return "", err
        }
        switch {
        case p.Number == 1 && !p.Echo && p.Text == "Password:":
            return os.Getenv("DB_PAM_PASSWORD"), nil
        case p.Number == 2 && p.Text == "Verification code:":
            return os.Getenv("DB_PAM_CODE"), nil
        default:
            return "", errors.New("unrecognized PAM question")
        }
    },
}))
if err != nil {
    return err
}
connector, err := mariadb.NewConnector(cfg)
if err != nil {
    return err
}
db := sql.OpenDB(connector)
defer db.Close()
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
return db.PingContext(ctx)
```

This example requires `context`, `database/sql`, `errors`, `os`, `time` and the
driver imports. Adapt prompt text to your PAM stack. A real one-time-code provider
must obtain a fresh value for each connection rather than reuse the environment
variable shown here. The callback may run concurrently on different connections,
must honor its context while waiting for input, and must never log responses.
`PAMPrompt.Text` is untrusted server text; `Last` is a hint, not proof of success.
Prompts/responses are capped at 64 KiB and responses cannot contain NUL. The
default prompt budget is eight, with a maximum of 64. A server may switch before
its first question; the driver acknowledges that empty switch without calling
your handler. Only a subsequent valid server OK completes authentication.

### Kerberos GSSAPI

The core driver defines the policy/provider interface without linking native
libraries. The optional provider import is:

```go
import "github.com/aantich/go-mariadb/gssapi"

// cfg contains the SQL username, address, database and verified TLS settings.
err := cfg.Apply(mariadb.WithGSSAPI(mariadb.GSSAPIConfig{
    ServicePrincipal: "mariadb/db.example.org@EXAMPLE.ORG",
    NewClient: gssapi.NewClient,
}))
```

Check `err`, then create the connector and use a bounded PingContext as above.
Acquire Kerberos credentials through your OS's normal `kinit`/credential-cache
workflow. The SQL password is not used. The exact expected service principal is
required before acquiring credentials; hostname discovery from the server's
challenge is deliberately absent. Configuring GSSAPI also requires this method:
password-only success and switches to another plugin are rejected. The initial
default-plugin handshake sends no password proof.

The native provider requires cgo and either the macOS GSS framework or Linux
GSSAPI headers/runtime (`libkrb5-dev` on Debian/Ubuntu). Building an application
that imports only the core driver adds no native dependency. Unsupported platforms
or cgo-disabled builds of the optional provider return `gssapi.ErrUnavailable`.
Windows SSPI/NTLM is not implemented. `AllowSPNEGO` is opt-in and retains a
Kerberos-only credential policy; credential delegation is never requested.

The exchange requires mutual authentication before accepting SQL OK, limits
tokens to 1 MiB and defaults to 16 provider steps (maximum 64). GSSAPI verifies
identity; it does not encrypt subsequent SQL, so configure TLS for confidentiality.
This provider does not supply GSS channel bindings. Authentication errors retain
server code/SQLSTATE but suppress server text that could echo credentials. This
protection remains in force across later authentication-plugin switches and
also suppresses peer-controlled plugin names and public-key diagnostic text.
Errors returned by application callbacks/providers retain their original identity.

Cancellation stops SQL authentication promptly. Native credential/KDC work already
in progress may finish later under the OS's retry policy; one worker exclusively
owns and eventually releases that context, without access to the SQL socket.
`gssapi.NewClient` shares a default bound of **32 native contexts** across the
process, including canceled calls whose native cleanup is still running. Waiting
for capacity honors the connection context and starts no native worker. SQL
pool limits remain useful, but do not bound work left behind by canceled connects.
To choose an independent capacity budget, create a provider once and share it
across the relevant connectors:

```go
provider, err := gssapi.NewProvider(16)
if err != nil { return err }
err = cfg.Apply(mariadb.WithGSSAPI(mariadb.GSSAPIConfig{
    ServicePrincipal: "mariadb/db.example.org@EXAMPLE.ORG",
    NewClient: provider.NewClient,
}))
```

Do not construct a provider per connection: each provider has an independent
budget. A returned native client holds its slot until Close and actual cleanup;
the core driver closes it after each authentication exchange. Applications
supplying a custom provider must preserve these lifetime, capacity, cancellation
and mutual authentication requirements. Policies/callbacks are copied into connector
configuration and are never included in a formatted DSN. Generic plugins may
implement `AuthPluginCloser` and `AuthPluginValidator` for equivalent cleanup and
local-completion checks.

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
call. Use `PrepareNative` below to retain the server statement. The matched benchmark
prebuilds SQL batches and TSV data, and prebuilds the native/prepared input matrix.
Native packet encoding remains timed. Its results compare these API alternatives;
they do not isolate wire-format costs.

**Bulk execution does not promise transaction atomicity.** For an application
transaction, execute BEGIN/COMMIT/ROLLBACK on the same pinned `sql.Conn`. A server
error may follow partial effects. An uncertain write outcome after disconnect or
cancellation must be resolved by application policy; blindly retrying can duplicate
effects. The driver retires canceled connections rather than attempting reuse.

## Per-unit bulk results

Apply `mariadb.WithBulkUnitResults(true)` before creating the connector. This
requests both bulk capabilities; it also enables ordinary aggregate `BulkExec`.
Use the pinned connection from the bulk example:

```go
units, err := mariadb.BulkExecUnits(ctx, conn,
    "INSERT INTO people(name) VALUES (?)", [][]any{{"Ada"}, {"Grace"}})
if err != nil { return err }
for _, unit := range units {
    fmt.Println(unit.InsertID, unit.RowsAffected) // uint64, server wire order
}
```

These are server execution units, not per-row error records. Do not assume one
unit per input for every DML optimization. Explicitly supplied IDs may have a
zero insert-ID counter. A server error returns no complete unit slice and can
follow partial effects. Use BEGIN/COMMIT/ROLLBACK on the same connection for
atomicity. RETURNING and multiple result sets are excluded from this helper;
use ordinary QueryContext for returning rows.

## Stream prepared parameters

`ExecStream` accepts ordinary arguments and explicit `StreamParam` values. It
uses COM_STMT_SEND_LONG_DATA without materializing the complete reader:

```go
file, err := os.Open("attachment.bin")
if err != nil { return err }
defer file.Close()
result, err := mariadb.ExecStream(ctx, conn,
    "INSERT INTO attachments(id, body) VALUES (?, ?)", uint64(7),
    mariadb.StreamParam{Reader: file, MaxBytes: 8 << 20, ChunkBytes: 64 << 10})
if err != nil { return err }
fmt.Println(result.RowsAffectedUnsigned())
```

No configuration flag is required. MaxBytes=0 uses the connection's outbound
packet limit; positive limits must not exceed it. ChunkBytes=0 uses 64 KiB;
explicit chunks may be at most 1 MiB and are clamped to the packet limit. The
clamp reserves the LONG_DATA command header and keeps its body strictly below
the maximum, including when MaxAllowedPacket is explicitly configured.
The server still limits the **complete** parameter with `max_allowed_packet`.
An empty reader binds an empty value; `nil` binds SQL NULL. Pass `StreamParam`
as a value to this helper, not to ordinary Exec or through driver.Valuer.

Readers remain caller-owned and are never closed. The driver checks cancellation
around reads, but cannot forcibly interrupt an arbitrary blocking Read. Arrange
for a network/pipe reader to return when its request is canceled. Reader errors
reset the unexecuted upload; reset/transport failure retires the connection.
Sources are one-shot: an error never requests automatic replay, and applications
must resolve ambiguous execution before creating a new reader and retrying.
The driver may consume one byte beyond MaxBytes to detect an oversized source.

## Reuse a native prepared statement

For repeated bulk/unit/stream executions of the same SQL, prepare once on a
pinned `sql.Conn`. Enable `WithBulkUnitResults(true)` for both bulk methods, or
`WithBulkOperations(true)` for aggregate bulk only; streamed execution needs no
additional capability. With `conn` obtained from the configured pool:

```go
stmt, err := mariadb.PrepareNative(ctx, conn,
    "INSERT INTO people(id, name) VALUES (?, ?)")
if err != nil { return err }
defer stmt.Close() // Prefer closing before returning conn to the pool.

for _, batch := range [][][]any{
    {{uint64(1), "Ada"}, {uint64(2), "Grace"}},
    {{uint64(3), "Edsger"}},
} {
    result, err := stmt.BulkExec(ctx, batch)
    if err != nil { return err }
    fmt.Println(result.RowsAffectedUnsigned())
}
// On a connection with unit results enabled:
units, err := stmt.BulkExecUnits(ctx, [][]any{{uint64(4), "Barbara"}})
if err != nil { return err }
fmt.Println(units)

// A handle can also execute ordinary/reader parameters. Supply a fresh source
// on every call; the driver never closes the reader or automatically replays it.
_, err = stmt.ExecStream(ctx, uint64(5),
    mariadb.StreamParam{Reader: strings.NewReader("Margaret"), MaxBytes: 100})
if err != nil { return err }
```

These methods preserve the one-shot helpers' validation, unsigned results,
transaction, partial-failure and reader contracts. Empty bulk input sends nothing.
Types, signedness and NULL status may change between calls. Managed reset reparses
the query lazily and checks arity; a stale close cannot close a recycled ID.
Use ordinary `sql.Stmt` for queries returning rows and ordinary scalar execution.

The handle owns a normal `sql.Stmt`, leaves its connection open on close and can
be closed safely after `conn.Close`; further nonempty executions then return
`sql.ErrConnDone`. Methods serialize concurrent calls; canceling a queued call
leaves the active call running. Do not reenter the same handle from a reader or
Valuer. `Close` is idempotent and waits for active work. It has no context, as
with `sql.Stmt.Close`; use execution cancellation and a configured `WriteTimeout`
to bound stalled cleanup. Close active Rows before preparing/using the handle.

If its physical connection has another caller's active Rows, `Close` releases the
handle locally and defers server cleanup until those Rows close. It preserves
their packet sequence and borrowed values. A deferred cleanup failure retires
the connection and is reported by `Rows.Close`/`Rows.Err`; check those errors.

Reusing the handle removes repeated prepare/close commands. Its throughput and
allocation effects are [not measured yet](STAGE4_STATEMENT_REUSE.md).

## Server cursors

Create a separate SELECT pool with `cfg.Apply(mariadb.WithServerCursors(128))`
before NewConnector. Its Query calls prepare a read-only cursor, including
parameterless queries; Exec remains ordinary. Use the standard row loop shown
above, including Scan, Scanner, RawBytes, ParseTime and deferred Close.

Fetch size must be 1–65535 rows. Early Close drains only the already requested
batch, then resets the statement and waits for its acknowledgement. Reusing
prepared statements, managed-session reprepare and metadata changes are supported.
Cancellation retires the connection through fetching and cleanup. Close rows
before issuing another command on that connection.

MariaDB materializes these cursors on the server. They bound in-flight client
row counts, not individual row bytes or server storage, and add a round trip per
fetch. Measure your workload before choosing them. Procedures, multi-result
queries and queries that do not open a cursor are excluded; use an ordinary pool
for those operations. No silent fallback changes the requested cursor semantics.
If materialization fails after metadata is sent, the returned `*MySQLError`
retains its MariaDB number and SQLSTATE. A complete SQL error leaves the
connection synchronized; a malformed response retires it.

## Progress reports

Apply `mariadb.WithProgressReports(true)` before NewConnector. On a pinned
connection:

```go
result, err := mariadb.ExecWithProgress(ctx, conn, func(p mariadb.Progress) error {
    fmt.Printf("stage %d/%d: %.3f%% %s\n",
        p.Stage, p.MaxStage, float64(p.Thousandths)/1000, p.Info)
    return nil
}, "ALTER TABLE people ADD COLUMN active BOOLEAN DEFAULT TRUE, ALGORITHM=COPY")
// Inspect result even when err is non-nil: a callback can fail after SQL succeeds.
if result != nil { fmt.Println(result.RowsAffectedUnsigned()) }
if err != nil { return err }
```

Only selected MariaDB operations emit progress. No reports is a valid success;
this option does not change global/session reporting settings. Progress describes
the current stage, not total completion; snapshots own their message strings.
Callbacks run synchronously, must return promptly/cooperate with cancellation
and must not reenter this connection.

A callback error suppresses later calls and is returned **after draining the
terminal SQL response**. It does not cancel or roll back SQL. A callback panic
becomes `*ProgressCallbackPanic`; its Error omits the panic value. If SQL also
fails, errors.Join preserves both errors for errors.Is/errors.As, including
MariaDB error number/SQLSTATE. A callback's own `driver.ErrBadConn`, including a
wrapped one, remains an application error and does not retire a healthy connection
or request automatic replay. Cancellation or protocol/transport failure retires
the connection. Never infer that an error means the DDL had no effect.

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

Ordinary pool reuse checks liveness. Enable the managed policy below when each
borrower requires a clean session; pin a `sql.Conn` for a request that intentionally
uses several commands with shared session state.

## Clean managed reuse

```go
cfg.DBName = "app" // Required: RESET does not clear the current database.
if err := cfg.Apply(mariadb.WithSessionReset(mariadb.SessionResetPolicy{
    Role: "app_role", // Literal role name; omit to restore ROLE NONE.
    InitSQL: []string{"SET @application='api'"},
})); err != nil { return err }
connector, err := mariadb.NewConnector(cfg)
if err != nil { return err }
db := sql.OpenDB(connector)
defer db.Close()
```

Fresh managed connections initialize in the same order as reset: charset and
collation, role, database, ordered `Config.Params`, then `InitSQL`. Use
`cfg.Apply(mariadb.AddParam(...))` when parameter order matters. Initialization
SQL is trusted application configuration, runs again on every checkout and should
be idempotent. It must leave no transaction open. The role is activated before
selecting a database whose privileges may depend on that role.

Clean checkout rolls back unfinished transactions and removes user variables,
temporary tables and server prepared statements. Existing Go statement handles
reprepare lazily, validating their parameter count before execution. Reset adds
round trips and reprepare work; enable it for the isolation contract you need.
Group related commands on one pinned connection to establish a request boundary.

For explicit cleanup on a managed pinned connection, first close rows and finish
any `sql.Tx`, then call `mariadb.ResetSession(ctx, conn)`. The caller still owns
`conn`. A reset/restoration error retires its physical connection; cancellation
never revives it. The helper preserves the original server/context error and does
not request automatic replay. Automatic pool checkout marks a retired connection
with Go's `ErrBadConn` before any caller query is sent, allowing safe replacement.

## Observe session changes

```go
if err := cfg.Apply(mariadb.WithSessionTracking(true)); err != nil { return err }
if err := cfg.Apply(mariadb.AddParam("session_track_system_variables",
    "'time_zone,sql_mode,autocommit'")); err != nil { return err }
// Construct the connector after applying options, then borrow conn from db.
state, err := mariadb.InspectSession(ctx, conn)
if err != nil { return err }
fmt.Println(state.Generation, state.ObservedSchema, state.ObservedVariables)
```

Inspect only after closing active rows, on the same pinned connection that ran
the commands. The snapshot sends no SQL, owns its maps/bytes and includes raw
tracking blocks for future types. These are **observations**, not a complete
mirror of arbitrary SQL state. Changing server tracker settings can make earlier
observations incomplete or stale. `WithSessionTrackLimit` bounds each payload and
the retained variable dictionary (64 KiB each by default); block/name counts are
also bounded. Malformed tracking data retires the stream.

## Native column metadata

```go
if err := cfg.Apply(mariadb.WithExtendedMetadata(true)); err != nil { return err }
// Create the connector with this configuration, then borrow conn.
columns, err := mariadb.Describe(ctx, conn, "SELECT id, address FROM people WHERE id=?")
if err != nil { return err }
for _, column := range columns {
    fmt.Println(column.Name, column.DatabaseType, column.Attributes)
}
```

`Describe` prepares and closes a transient statement without executing it or
binding parameters. Returned attributes are owned. Ordinary `rows.ColumnTypes()`
also reports native names when sent by the server. Raw values and Scanner types
retain their existing behavior. UUID, INET4, INET6 and JSON metadata are covered
by actual table tests, including cache omission, DDL reprepare and reset.

`Describe` reports the native/wire database type independently of scan options.
For example, with `TinyInt1IsBool(true)`, a signed `TINYINT(1)` column is still
`TINYINT` in `Describe`, while `ColumnTypes` calls it `BOOLEAN` and selects a
boolean scan type. Use `Describe` for database type identity and `ColumnTypes`
when choosing destinations for the configured row scanner.

MariaDB 12.3 sends VECTOR as binary VARCHAR without an extended type-name
attribute, so it is accurately reported as VARBINARY; VECTOR values still round
trip as bytes. Use a deliberate catalog query when an application needs that
table declaration. The driver does not infer identity from a value's length.
An explicitly requested capability missing from the greeting returns
`ErrUnsupportedCapability` during connection setup.

## Migration and readiness

Change the module import and SQL registration name from the original driver to
this package. `MySQLDriver` remains a deprecated alias for `MariaDBDriver`, and
some error/internal type names remain inherited. No `mysql` driver is registered
by this package. Preserve upstream license notices when redistributing it.

Consult the [implementation record](DEDICATED_DRIVER.md),
[roadmap](ROADMAP.md) and [coverage limits](../validation/COVERAGE.md).
Current automated checks do not certify production applications, all MariaDB
versions, failover topologies or external authentication services.
