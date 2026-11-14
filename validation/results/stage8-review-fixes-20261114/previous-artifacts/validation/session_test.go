// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

func requireInstrumentation() bool { return os.Getenv("GO_MARIADB_REQUIRE_INSTRUMENTATION") == "1" }

func managedConfig(t *testing.T, mode string) *mariadb.Config {
	t.Helper()
	cfg := driverConfig(t, mode)
	if mode == "legacy" {
		cfg.DialFunc = maskEOFGreeting
	}
	if mode == "uncached" {
		cfg.DialFunc = maskSessionCacheGreeting
	}
	cfg.Params["sql_mode"] = "'STRICT_TRANS_TABLES'"
	cfg.Params["session_track_system_variables"] = "'time_zone,sql_mode,autocommit'"
	if err := cfg.Apply(mariadb.WithSessionReset(mariadb.SessionResetPolicy{InitSQL: []string{"SET @go_stage2_init='ready'"}}), mariadb.WithSessionTracking(true), mariadb.WithExtendedMetadata(true), mariadb.WithBulkOperations(true)); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMariaDBManagedSessionIsolation(t *testing.T) {
	for _, mode := range []string{"text", "compressed", "tls", "socket", "x509", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			db := openConfig(t, managedConfig(t, mode))
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			mustExec(t, db, "CREATE TABLE IF NOT EXISTS go_stage2_rollback(v INT)")
			mustExec(t, db, "DELETE FROM go_stage2_rollback")
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			initial, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil {
				t.Fatal(err)
			}
			var before, after uint64
			if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&before); err != nil {
				t.Fatal(err)
			}
			stmt, err := conn.PrepareContext(t.Context(), "SELECT ?+1")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_stage2_private(v INT)")
			mustExec(t, conn, "SET @go_private_request='secret', @go_stage2_init='dirty'")
			mustExec(t, conn, "START TRANSACTION")
			mustExec(t, conn, "INSERT INTO go_stage2_rollback VALUES (1)")
			mustExec(t, conn, "SET NAMES latin1")
			mustExec(t, conn, "SET time_zone='+03:00',sql_mode='ANSI_QUOTES'")
			mustExec(t, conn, "USE go_mariadb_upstream")
			if err := mariadb.ResetSession(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			var leaked sql.NullString
			var schema, charset, zone, modeValue, init string
			var count int
			if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID(),DATABASE(),@@character_set_client,@@time_zone,@@sql_mode,@go_stage2_init,@go_private_request").Scan(&after, &schema, &charset, &zone, &modeValue, &init, &leaked); err != nil {
				t.Fatal(err)
			}
			if before != after || schema != "go_mariadb_validation" || charset != "utf8mb4" || zone != "+00:00" || modeValue != "STRICT_TRANS_TABLES" || init != "ready" || leaked.Valid {
				t.Fatalf("bad restoration: id=%d/%d schema=%s charset=%s zone=%s mode=%s init=%s leaked=%v", before, after, schema, charset, zone, modeValue, init, leaked.Valid)
			}
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_stage2_rollback").Scan(&count); err != nil || count != 0 {
				t.Fatalf("rollback count=%d error=%v", count, err)
			}
			if _, err := conn.ExecContext(t.Context(), "SELECT * FROM go_stage2_private"); err == nil {
				t.Fatal("private temporary table survived RESET")
			}
			for i := range 3 {
				var got int
				if err := stmt.QueryRowContext(t.Context(), i).Scan(&got); err != nil || got != i+1 {
					t.Fatalf("reprepared query: got=%d err=%v", got, err)
				}
				if err := mariadb.ResetSession(t.Context(), conn); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil || snapshot.Generation != initial.Generation+4 {
				t.Fatalf("generation=%d error=%v", snapshot.Generation, err)
			}
			bulk, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_stage2_rollback VALUES (?)", [][]any{{int64(10)}, {int64(11)}})
			if err != nil || bulk.RowsAffectedUnsigned() != 2 {
				t.Fatalf("bulk with managed metadata/tracking: %v", err)
			}
			var sum int
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*),SUM(v) FROM go_stage2_rollback").Scan(&count, &sum); err != nil || count != 2 || sum != 21 {
				t.Fatalf("managed bulk contents: %d/%d %v", count, sum, err)
			}
			mustExec(t, conn, "DELETE FROM go_stage2_rollback")
			mustExec(t, conn, "SET @go_private_request='next request'")
			conn.Close()
			if err := db.QueryRowContext(t.Context(), "SELECT @go_private_request").Scan(&leaked); err != nil || leaked.Valid {
				t.Fatalf("pool checkout leaked state: %v %v", leaked, err)
			}
			poolStmt, err := db.PrepareContext(t.Context(), "SELECT ?+7")
			if err != nil {
				t.Fatal(err)
			}
			defer poolStmt.Close()
			for i := range 3 {
				var got int
				if err := poolStmt.QueryRowContext(t.Context(), i).Scan(&got); err != nil || got != i+7 {
					t.Fatalf("pooled statement generation: %d %v", got, err)
				}
			}
			write, err := db.PrepareContext(t.Context(), "INSERT INTO go_stage2_rollback VALUES (?)")
			if err != nil {
				t.Fatal(err)
			}
			defer write.Close()
			for i := range 3 {
				result, err := write.ExecContext(t.Context(), i)
				if err != nil {
					t.Fatal(err)
				}
				if n, err := result.RowsAffected(); err != nil || n != 1 {
					t.Fatalf("prepared write result: %d %v", n, err)
				}
			}
			if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*),SUM(v) FROM go_stage2_rollback").Scan(&count, &sum); err != nil || count != 3 || sum != 3 {
				t.Fatalf("reset duplicated/lost prepared writes: %d/%d %v", count, sum, err)
			}
		})
	}
}

func TestMariaDBDefaultSessionPolicy(t *testing.T) {
	db := openDB(t, "text")
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, "SET @go_stage2_default='persistent'")
	if err := mariadb.ResetSession(t.Context(), conn); err == nil {
		t.Fatal("unmanaged session accepted explicit managed reset")
	}
	conn.Close()
	var value string
	if err := db.QueryRowContext(t.Context(), "SELECT @go_stage2_default").Scan(&value); err != nil || value != "persistent" {
		t.Fatalf("ordinary checkout behavior changed: %q %v", value, err)
	}
}

func TestMariaDBSessionObservations(t *testing.T) {
	for _, mode := range []string{"text", "compressed", "tls", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			cfg := driverConfig(t, mode)
			if mode == "legacy" {
				cfg.DialFunc = maskEOFGreeting
			}
			cfg.Params["session_track_system_variables"] = "'time_zone,sql_mode'"
			cfg.Params["session_track_schema"] = "1"
			cfg.Params["session_track_state_change"] = "1"
			cfg.Params["session_track_transaction_info"] = "'CHARACTERISTICS'"
			cfg.MultiStatements = true
			if err := cfg.Apply(mariadb.WithSessionTracking(true)); err != nil {
				t.Fatal(err)
			}
			db := openConfig(t, cfg)
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mustExec(t, conn, "SET time_zone='+02:00'")
			first, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil {
				t.Fatal(err)
			}
			if first.ObservedVariables["time_zone"] != "+02:00" || len(first.Changes) == 0 {
				t.Fatalf("missing variable observation: %+v", first)
			}
			first.ObservedVariables["time_zone"] = "forged"
			original := append([]byte(nil), first.Changes[0].Data...)
			for i := range first.Changes {
				if len(first.Changes[i].Data) > 0 {
					first.Changes[i].Data[0] ^= 0xff
				}
			}
			second, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil || second.ObservedVariables["time_zone"] != "+02:00" {
				t.Fatal("returned observations alias internal state", err)
			}
			if len(second.Changes) == 0 || !bytes.Equal(second.Changes[0].Data, original) {
				t.Fatal("returned raw observations alias internal state")
			}
			mustExec(t, conn, "USE go_mariadb_upstream")
			third, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil || third.ObservedSchema != "go_mariadb_upstream" || third.Revision <= second.Revision {
				t.Fatalf("schema observation: %+v %v", third, err)
			}
			mustExec(t, conn, "SET time_zone='+04:00'; USE go_mariadb_validation")
			combined, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil || combined.ObservedSchema != "go_mariadb_validation" || combined.ObservedVariables["time_zone"] != "+04:00" {
				t.Fatalf("multiple-result observations: %+v %v", combined, err)
			}
			mustExec(t, conn, "START TRANSACTION READ ONLY")
			if _, err := mariadb.InspectSession(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			mustExec(t, conn, "ROLLBACK")
			if _, err := conn.ExecContext(t.Context(), "SELECT nonexistent_column"); err == nil {
				t.Fatal("server error accepted")
			}
			var value int
			if err := conn.QueryRowContext(t.Context(), "SELECT 42").Scan(&value); err != nil || value != 42 {
				t.Fatal("tracking desynchronized after error", err)
			}
		})
	}
}

func TestMariaDBNativeTypeMetadata(t *testing.T) {
	for _, mode := range []string{"text", "compressed", "tls", "socket", "legacy", "uncached"} {
		t.Run(mode, func(t *testing.T) {
			cfg := managedConfig(t, mode)
			db := openConfig(t, cfg)
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mustExec(t, conn, "DROP TABLE IF EXISTS go_stage2_types")
			mustExec(t, conn, "CREATE TABLE go_stage2_types(id INT PRIMARY KEY,u UUID,a INET6,b INET4,j JSON,v VECTOR(2),g GEOMETRY)")
			mustExec(t, conn, "INSERT INTO go_stage2_types VALUES(1,'123e4567-e89b-12d3-a456-426614174000','::1','127.0.0.1','{\"ok\":true}',VEC_FromText('[1,2]'),ST_GeomFromText('POINT(1 2)'))")
			columns, err := mariadb.Describe(t.Context(), conn, "SELECT * FROM go_stage2_types WHERE id=?")
			if err != nil {
				t.Fatal(err)
			}
			// MariaDB 12.3 sends VECTOR as binary VARCHAR without an extended
			// type-name attribute. Preserve that fact rather than infer a type
			// from the value's byte length or silently query system catalogs.
			want := []string{"INT", "UUID", "INET6", "INET4", "JSON", "VARBINARY", "GEOMETRY"}
			var catalogType string
			if err := conn.QueryRowContext(t.Context(), "SELECT DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='go_stage2_types' AND COLUMN_NAME='v'").Scan(&catalogType); err != nil || catalogType != "vector" {
				t.Fatalf("VECTOR fixture identity: %s %v", catalogType, err)
			}
			if len(columns) != len(want) {
				t.Fatalf("metadata columns=%d", len(columns))
			}
			for i, column := range columns {
				if column.DatabaseType != want[i] {
					t.Errorf("column %s type=%s want=%s attrs=%+v", column.Name, column.DatabaseType, want[i], column.Attributes)
				}
			}
			if len(columns[1].Attributes) == 0 {
				t.Fatal("UUID native attributes missing")
			}
			columns[1].Attributes[0].Value = "forged"
			again, err := mariadb.Describe(t.Context(), conn, "SELECT u FROM go_stage2_types")
			if err != nil || again[0].DatabaseType != "UUID" {
				t.Fatal("metadata ownership", err)
			}
			stmt, err := conn.PrepareContext(t.Context(), "SELECT u,a,b,j,v,g FROM go_stage2_types WHERE id=?")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			for attempt := range 3 {
				rows, err := stmt.QueryContext(t.Context(), 1)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				types, err := rows.ColumnTypes()
				if err != nil {
					t.Fatal(err)
				}
				for i, typ := range types {
					if typ.DatabaseTypeName() != want[i+1] {
						t.Fatalf("ColumnTypes %d=%s", i, typ.DatabaseTypeName())
					}
				}
				if !rows.Next() {
					t.Fatal("missing type values", rows.Err())
				}
				var uuid, ipv6, ipv4, jsonValue string
				var vector, geometry []byte
				if err := rows.Scan(&uuid, &ipv6, &ipv4, &jsonValue, &vector, &geometry); err != nil {
					t.Fatal(err)
				}
				if uuid != "123e4567-e89b-12d3-a456-426614174000" || ipv6 != "::1" || ipv4 != "127.0.0.1" || !strings.Contains(jsonValue, "true") || len(vector) != 8 || len(geometry) == 0 {
					t.Fatal("native metadata changed value decoding")
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				if attempt == 1 {
					if err := mariadb.ResetSession(t.Context(), conn); err != nil {
						t.Fatal(err)
					}
				}
			}
			mustExec(t, conn, "ALTER TABLE go_stage2_types MODIFY u VARCHAR(40)")
			rows, err := stmt.QueryContext(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			types, err := rows.ColumnTypes()
			rows.Close()
			if err != nil || types[0].DatabaseTypeName() != "VARCHAR" {
				t.Fatalf("DDL metadata invalidation: %+v %v", types, err)
			}
		})
	}
}

// Change only the advertised cache capability before negotiation. Extended
// metadata must also work when the server sends definitions on every execution.
func maskSessionCacheGreeting(ctx context.Context, network, address string) (_ net.Conn, err error) {
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var header [4]byte
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 2 || n > 4096 {
		return nil, fmt.Errorf("unexpected greeting size %d", n)
	}
	body := make([]byte, n)
	if _, err = io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	end := bytes.IndexByte(body[1:], 0) + 1
	highWord := end + 28
	if end < 1 || highWord+4 > len(body) {
		return nil, errors.New("invalid fixture greeting")
	}
	body[highWord] &^= 16
	conn.SetReadDeadline(time.Time{})
	return &greetingControl{Conn: conn, reader: io.MultiReader(bytes.NewReader(append(header[:], body...)), conn), received: &atomic.Uint64{}}, nil
}

func TestMariaDBManagedRestorationFailure(t *testing.T) {
	plain := openDB(t, "text")
	mustExec(t, plain, "DROP TABLE IF EXISTS go_stage2_must_not_execute")
	mustExec(t, plain, "CREATE TABLE IF NOT EXISTS go_stage2_init_source(v INT)")
	cfg := driverConfig(t, "text")
	if err := cfg.Apply(mariadb.WithSessionReset(mariadb.SessionResetPolicy{InitSQL: []string{"SET @go_stage2_n=(SELECT COUNT(*) FROM go_stage2_init_source)"}})); err != nil {
		t.Fatal(err)
	}
	db := openConfig(t, cfg)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mustExec(t, conn, "DROP TABLE go_stage2_init_source")
	err = mariadb.ResetSession(t.Context(), conn)
	var server *mariadb.MySQLError
	if !errors.As(err, &server) || server.Number != 1146 {
		t.Fatalf("restoration error identity: %v", err)
	}
	if err := conn.PingContext(t.Context()); err == nil {
		t.Fatal("partially restored connection reused")
	}
	mustExec(t, plain, "CREATE TABLE go_stage2_init_source(v INT)")
	// A pool checkout must discard its failed reset before any caller SQL can
	// run. database/sql ignores SessionResetter errors without ErrBadConn.
	conn.Close()
	pooled := openConfig(t, cfg)
	pooled.SetMaxOpenConns(1)
	pooled.SetMaxIdleConns(1)
	var id uint64
	if err := pooled.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	mustExec(t, plain, "DROP TABLE go_stage2_init_source")
	_, err = pooled.ExecContext(t.Context(), "CREATE TABLE go_stage2_must_not_execute(v INT)")
	if !errors.As(err, &server) || server.Number != 1146 {
		t.Fatalf("pooled restoration error: %v", err)
	}
	var count int
	if err := plain.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='go_stage2_must_not_execute'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("caller SQL executed after failed checkout: %d %v", count, err)
	}
	mustExec(t, plain, "CREATE TABLE go_stage2_init_source(v INT)")
}

func TestMariaDBManagedRoleAndSchema(t *testing.T) {
	cfg := driverConfig(t, "text")
	cfg.User = "gomdb_roleonly"
	unmanaged, err := mariadb.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	control := sql.OpenDB(unmanaged)
	err = control.PingContext(t.Context())
	control.Close()
	var denied *mariadb.MySQLError
	if !errors.As(err, &denied) || denied.Number != 1044 {
		t.Fatalf("role fixture unexpectedly has direct schema access: %v", err)
	}
	if err := cfg.Apply(mariadb.WithSessionReset(mariadb.SessionResetPolicy{Role: "gomdb_session_role"})); err != nil {
		t.Fatal(err)
	}
	db := openConfig(t, cfg)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for range 2 {
		var role, schema string
		if err := conn.QueryRowContext(t.Context(), "SELECT CURRENT_ROLE(),DATABASE()").Scan(&role, &schema); err != nil || role != "gomdb_session_role" || schema != "go_mariadb_validation" {
			t.Fatalf("role before schema: %s/%s %v", role, schema, err)
		}
		if err := mariadb.ResetSession(t.Context(), conn); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMariaDBSessionInstrumentation(t *testing.T) {
	if !requireInstrumentation() {
		t.Skip("PROFILE: performance_schema requires the isolated instrumentation fixture")
	}
	cfg := managedConfig(t, "text")
	cfg.ConnectionAttributes = "go_stage:session-foundations"
	db := openConfig(t, cfg)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var enabled int
	if err := conn.QueryRowContext(t.Context(), "SELECT @@performance_schema").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("instrumentation unavailable: %d %v", enabled, err)
	}
	var count int
	if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM performance_schema.threads WHERE PROCESSLIST_ID=CONNECTION_ID()").Scan(&count); err != nil || count != 1 {
		t.Fatalf("connection instrumentation missing: %d %v", count, err)
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM performance_schema.session_connect_attrs WHERE PROCESSLIST_ID=CONNECTION_ID() AND ATTR_NAME='go_stage' AND ATTR_VALUE='session-foundations'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("connection attributes missing: %d %v", count, err)
	}
	if _, err := conn.ExecContext(t.Context(), "DO 1 /* go_stage2_instrumented */"); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM performance_schema.events_statements_history WHERE THREAD_ID=(SELECT THREAD_ID FROM performance_schema.threads WHERE PROCESSLIST_ID=CONNECTION_ID()) AND SQL_TEXT LIKE '%go_stage2_instrumented%'").Scan(&count); err != nil || count == 0 {
		t.Fatalf("statement instrumentation missing: %d %v", count, err)
	}
	if err := mariadb.ResetSession(context.Background(), conn); err != nil {
		t.Fatal(fmt.Errorf("instrumented reset: %w", err))
	}
}
