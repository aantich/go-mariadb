// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	"github.com/aantich/go-mariadb/cdc"
)

func compatibilityVersion(t *testing.T, db *sql.DB) (version, family string, major, minor int) {
	t.Helper()
	if err := db.QueryRowContext(t.Context(), "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimPrefix(version, "5.5.5-"), ".")
	if len(parts) < 3 || !strings.Contains(version, "MariaDB") {
		t.Fatal("expected numeric MariaDB version", version)
	}
	var err error
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	family = fmt.Sprintf("%d.%d", major, minor)
	if expected := os.Getenv("GO_MARIADB_MATRIX_FAMILY"); expected != "" && expected != family {
		t.Fatal("matrix used wrong server", expected, version)
	}
	return
}

func TestMariaDBStage7Compatibility(t *testing.T) {
	db := openDB(t, "text")
	version, family, major, minor := compatibilityVersion(t, db)
	atLeast := func(m, n int) bool { return major > m || major == m && minor >= n }
	units, parsec, vector := atLeast(11, 5), atLeast(11, 6), atLeast(11, 7)
	mustExec(t, db, "DROP TABLE IF EXISTS go_stage7_values")
	mustExec(t, db, "CREATE TABLE go_stage7_values(id BIGINT UNSIGNED PRIMARY KEY,d DECIMAL(38,9),s VARCHAR(100),b BLOB,n VARCHAR(10),dt DATETIME(6),j JSON,u UUID,ip INET6) ENGINE=InnoDB")
	t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE go_stage7_values") })
	date := time.Date(2026, 11, 12, 1, 2, 3, 123456000, time.UTC)
	mustExec(t, db, "INSERT INTO go_stage7_values VALUES(?,?,?,?,?,?,?,?,?)", uint64(^uint64(0)), "-12345678901234567890123456789.123456789", "hello 🌍", []byte{0, 1, 255}, nil, date, `{"n":7}`, "00112233-4455-6677-8899-aabbccddeeff", "2001:db8::1")
	for _, mode := range []string{"text", "compressed", "tls", "tls compressed"} {
		t.Run("values/"+mode, func(t *testing.T) {
			base := mode
			if mode == "tls compressed" {
				base = "tls"
			}
			cfg := driverConfig(t, base)
			cfg.ParseTime = true
			if mode == "tls compressed" {
				if err := cfg.Apply(mariadb.EnableCompression(true)); err != nil {
					t.Fatal(err)
				}
			}
			pool := openConfig(t, cfg)
			for _, prepared := range []bool{false, true} {
				t.Run(fmt.Sprint("prepared=", prepared), func(t *testing.T) {
					const query = "SELECT id,d,s,b,n,dt,JSON_VALUE(j,'$.n'),u,ip FROM go_stage7_values"
					var row *sql.Row
					if prepared {
						stmt, err := pool.PrepareContext(t.Context(), query)
						if err != nil {
							t.Fatal(err)
						}
						defer stmt.Close()
						row = stmt.QueryRowContext(t.Context())
					} else {
						row = pool.QueryRowContext(t.Context(), query)
					}
					var id uint64
					var decimal, text, uuid, ip string
					var blob []byte
					var nullable sql.NullString
					var gotDate time.Time
					var number int
					if err := row.Scan(&id, &decimal, &text, &blob, &nullable, &gotDate, &number, &uuid, &ip); err != nil {
						t.Fatal(err)
					}
					if id != ^uint64(0) || decimal != "-12345678901234567890123456789.123456789" || text != "hello 🌍" || string(blob) != string([]byte{0, 1, 255}) || nullable.Valid || !gotDate.Equal(date) || number != 7 || uuid != "00112233-4455-6677-8899-aabbccddeeff" || ip != "2001:db8::1" {
						t.Fatalf("value fidelity lost: %d %s %s %x %+v %v %d %s %s", id, decimal, text, blob, nullable, gotDate, number, uuid, ip)
					}
				})
			}
		})
	}
	for _, mode := range []string{"text", "compressed", "tls", "tls compressed", "legacy", "uncached"} {
		t.Run("native policies/"+mode, func(t *testing.T) {
			base := mode
			if mode == "tls compressed" {
				base = "tls"
			}
			cfg := managedConfig(t, base)
			if err := cfg.Apply(mariadb.WithServerCursors(2), mariadb.WithProgressReports(true)); err != nil {
				t.Fatal(err)
			}
			if mode == "tls compressed" {
				if err := cfg.Apply(mariadb.EnableCompression(true)); err != nil {
					t.Fatal(err)
				}
			}
			pool := openConfig(t, cfg)
			conn, err := pool.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			const create = "CREATE TEMPORARY TABLE go_stage7_native(id INT PRIMARY KEY,v BLOB)"
			mustExec(t, conn, create)
			stmt, err := mariadb.PrepareNative(t.Context(), conn, "INSERT INTO go_stage7_native(id,v) VALUES(?,?)")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			result, err := stmt.BulkExec(t.Context(), [][]any{{int64(1), []byte("a")}, {int64(2), []byte("b")}})
			if err != nil || result.RowsAffectedUnsigned() != 2 {
				t.Fatal(result, err)
			}
			if _, err := stmt.ExecStream(t.Context(), int64(3), mariadb.StreamParam{Reader: bytes.NewReader([]byte("c")), MaxBytes: 1}); err != nil {
				t.Fatal(err)
			}
			rows, err := conn.QueryContext(t.Context(), "SELECT id,v FROM go_stage7_native ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			var retained []byte
			for rows.Next() {
				var id int
				var value []byte
				if err := rows.Scan(&id, &value); err != nil {
					t.Fatal(err)
				}
				count++
				if id != count || string(value) != string(rune('a'+count-1)) {
					t.Fatal("cursor value", id, value)
				}
				if count == 1 {
					retained = value
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if count != 3 || string(retained) != "a" {
				t.Fatal("cursor ownership/count", count, retained)
			}
			calls := 0
			if _, err := mariadb.ExecWithProgress(t.Context(), conn, func(report mariadb.Progress) error { calls++; return nil }, "ALTER TABLE go_stage7_native ADD COLUMN extra INT DEFAULT 0, ALGORITHM=COPY"); err != nil || calls == 0 {
				t.Fatal("required progress report", calls, err)
			}
			before, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil {
				t.Fatal(err)
			}
			if err := mariadb.ResetSession(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			after, err := mariadb.InspectSession(t.Context(), conn)
			if err != nil || after.Generation <= before.Generation {
				t.Fatal("reset generation", before.Generation, after.Generation, err)
			}
			mustExec(t, conn, create)
			if result, err := stmt.BulkExec(t.Context(), [][]any{{int64(4), []byte("fresh")}}); err != nil || result.RowsAffectedUnsigned() != 1 {
				t.Fatal("retained handle did not reprepare", result, err)
			}
		})
	}
	t.Run("bulk units capability", func(t *testing.T) {
		cfg := driverConfig(t, "text")
		if err := cfg.Apply(mariadb.WithBulkUnitResults(true)); err != nil {
			t.Fatal(err)
		}
		pool := openConfig(t, cfg)
		conn, err := pool.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		result, err := mariadb.BulkExecUnits(t.Context(), conn, "UPDATE go_stage7_values SET s=?", [][]any{{"units"}})
		if units {
			if err != nil || len(result) != 1 || result[0].RowsAffected != 1 {
				t.Fatal("advertised unit results failed", result, err)
			}
		} else {
			if !errors.Is(err, mariadb.ErrUnsupportedCapability) || result != nil {
				t.Fatal("unavailable capability was not rejected", result, err)
			}
			scalar(t, pool, "SELECT s FROM go_stage7_values", "hello 🌍")
		}
	})
	t.Run("native metadata", func(t *testing.T) {
		cfg := driverConfig(t, "text")
		if err := cfg.Apply(mariadb.WithExtendedMetadata(true)); err != nil {
			t.Fatal(err)
		}
		pool := openConfig(t, cfg)
		conn, err := pool.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		columns, err := mariadb.Describe(t.Context(), conn, "SELECT j,u,ip FROM go_stage7_values")
		if err != nil || len(columns) != 3 {
			t.Fatal(columns, err)
		}
		for i, want := range []string{"JSON", "UUID", "INET6"} {
			if columns[i].DatabaseType != want {
				t.Fatal("native identity lost", columns)
			}
		}
	})
	for _, plugin := range []string{"ed25519", "parsec"} {
		t.Run("authentication/"+plugin, func(t *testing.T) {
			var present int
			if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_TYPE='AUTHENTICATION' AND PLUGIN_STATUS='ACTIVE' AND PLUGIN_NAME=?", plugin).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if plugin == "parsec" && !parsec {
				if present != 0 {
					t.Fatal("unexpected backported plugin; update expected profile")
				}
				return
			}
			if present != 1 {
				t.Fatal("required authentication fixture missing", plugin)
			}
			cfg := driverConfig(t, "tls")
			cfg.User = "gomdb_" + plugin
			pool := openConfig(t, cfg)
			scalar(t, pool, "SELECT 7", "7")
		})
	}
	t.Run("vector availability", func(t *testing.T) {
		mustExec(t, db, "DROP TABLE IF EXISTS go_stage7_vector")
		defer db.ExecContext(context.Background(), "DROP TABLE IF EXISTS go_stage7_vector")
		_, err := db.ExecContext(t.Context(), "CREATE TABLE go_stage7_vector(v VECTOR(3))")
		if vector {
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, "INSERT INTO go_stage7_vector VALUES(VEC_FromText('[1,2,3]'))")
			scalar(t, db, "SELECT VEC_ToText(v) FROM go_stage7_vector", "[1,2,3]")
		} else {
			var server *mariadb.MySQLError
			if !errors.As(err, &server) || (server.Number != 1064 && server.Number != 4161) {
				t.Fatal("expected unavailable VECTOR type", err)
			}
		}
	})
	marker, _ := json.Marshal(map[string]any{"version": version, "family": family, "expected_features": map[string]bool{"bulk_units": units, "parsec": parsec, "vector": vector}})
	t.Log("MARIADB_COMPATIBILITY " + string(marker))
}

// Stage 7's matrix owns its whole server. Current-profile full validation uses
// the existing separate CDC server instead, preserving the shared SQL service.
func stage7CDCConfig(t *testing.T, mode string) *mariadb.Config {
	t.Helper()
	var cfg *mariadb.Config
	if os.Getenv("GO_MARIADB_MATRIX_CONTROL") != "" {
		base := mode
		if mode == "tls compressed" {
			base = "tls"
		}
		cfg = driverConfig(t, base)
		if mode == "tls compressed" {
			if err := cfg.Apply(mariadb.EnableCompression(true)); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		if os.Getenv("GO_MARIADB_CDC_SOCKET") == "" {
			t.Fatal("owned CDC fixture required for Stage 7")
		}
		cfg = cdcConfig(t, strings.Contains(mode, "compressed"), strings.Contains(mode, "tls"))
	}
	return cfg
}

func stage7Checkpoint(t *testing.T, s *cdc.Stream) (*cdc.Checkpoint, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	writes := 0
	for {
		event, err := s.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if rows, ok := event.Data.(cdc.Rows); ok && (event.Type == cdc.WriteRowsV1 || event.Type == cdc.WriteRowsV2) {
			writes += len(rows.Changes)
		}
		if event.Checkpoint != nil {
			return event.Checkpoint, writes
		}
	}
}

func TestMariaDBStage7CDC(t *testing.T) {
	if os.Getenv("MARIADB_VALIDATION_CONFIG") == "" && os.Getenv("GO_MARIADB_CDC_SOCKET") == "" {
		t.Skip("isolated Stage 7 fixtures not configured")
	}
	for index, mode := range []string{"text", "compressed", "tls", "tls compressed"} {
		t.Run(mode, func(t *testing.T) {
			cfg := stage7CDCConfig(t, mode)
			db := openConfig(t, cfg)
			mustExec(t, db, "DROP TABLE IF EXISTS go_stage7_cdc")
			mustExec(t, db, "CREATE TABLE go_stage7_cdc(id INT PRIMARY KEY,v VARCHAR(30)) ENGINE=InnoDB")
			defer db.ExecContext(context.Background(), "DROP TABLE go_stage7_cdc")
			var text string
			if err := db.QueryRowContext(t.Context(), "SELECT @@global.gtid_binlog_pos").Scan(&text); err != nil {
				t.Fatal(err)
			}
			gtids, err := mariadb.ParseGTIDSet(text)
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, "INSERT INTO go_stage7_cdc VALUES(1,'first')")
			stream, err := cdc.Open(t.Context(), cfg, cdc.Options{ServerID: uint32(731710 + index), Start: cdc.Resume{GTIDs: gtids, UseGTID: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			checkpoint, writes := stage7Checkpoint(t, stream)
			if writes != 1 {
				t.Fatal("initial group", writes)
			}
			if err := stream.Acknowledge(t.Context(), checkpoint); err != nil {
				t.Fatal(err)
			}
			var thread uint64
			if err := db.QueryRowContext(t.Context(), "SELECT ID FROM INFORMATION_SCHEMA.PROCESSLIST WHERE COMMAND='Binlog Dump' AND DB=?", cfg.DBName).Scan(&thread); err != nil {
				t.Fatal("identify owned replication thread", err)
			}
			mustExec(t, db, fmt.Sprintf("KILL CONNECTION %d", thread))
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			if _, err := stream.Next(ctx); err == nil || err == io.EOF {
				t.Fatal("disconnected live source looked complete", err)
			}
			saved := checkpoint.Resume()
			if stream.LatestCheckpoint().Resume().GTIDs.String() != saved.GTIDs.String() || stream.AcknowledgedCheckpoint().Resume().GTIDs.String() != saved.GTIDs.String() {
				t.Fatal("disconnect changed durable progress")
			}
			mustExec(t, db, "ALTER TABLE go_stage7_cdc ADD COLUMN extra INT DEFAULT 9")
			mustExec(t, db, "INSERT INTO go_stage7_cdc(id,v) VALUES(2,'resumed')")
			reopened, err := cdc.Open(t.Context(), cfg, cdc.Options{ServerID: uint32(731720 + index), Start: saved, NonBlocking: true})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			rows, groups := 0, 0
			for _, event := range cdcReadAll(t, reopened) {
				if event.Checkpoint != nil {
					groups++
				}
				if data, ok := event.Data.(cdc.Rows); ok {
					rows += len(data.Changes)
					if len(data.Table.Columns) != 3 {
						t.Fatal("stale schema after resume")
					}
				}
			}
			if rows != 1 || groups != 2 {
				t.Fatal("resume duplicated or lost source work", rows, groups)
			}
		})
	}
}

func TestMariaDBStage7Restart(t *testing.T) {
	if os.Getenv("MARIADB_VALIDATION_CONFIG") == "" && os.Getenv("GO_MARIADB_CDC_SOCKET") == "" {
		t.Skip("isolated Stage 7 fixtures not configured")
	}
	cfg := stage7CDCConfig(t, "text")
	db := openConfig(t, cfg)
	mustExec(t, db, "DROP TABLE IF EXISTS go_stage7_restart")
	mustExec(t, db, "CREATE TABLE go_stage7_restart(id INT PRIMARY KEY) ENGINE=InnoDB")
	defer db.ExecContext(context.Background(), "DROP TABLE go_stage7_restart")
	mustExec(t, db, "INSERT INTO go_stage7_restart VALUES(1)")
	stmt, err := db.PrepareContext(t.Context(), "SELECT COUNT(*) FROM go_stage7_restart")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mustExec(t, tx, "INSERT INTO go_stage7_restart VALUES(2)")
	directory := os.Getenv("GO_MARIADB_MATRIX_CONTROL")
	if directory == "" {
		directory = os.Getenv("GO_MARIADB_CDC_CONTROL")
	}
	if directory == "" {
		t.Fatal("owned restart control required")
	}
	done := filepath.Join(directory, "restart.done")
	os.Remove(done)
	if err := os.WriteFile(filepath.Join(directory, "restart.request"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(35 * time.Second)
	for {
		if _, err := os.Stat(done); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned server restart deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("disconnected transaction reported a successful commit")
	}
	recoveryValue(t, db, "SELECT COUNT(*) FROM go_stage7_restart", 1)
	var count int
	if err := stmt.QueryRowContext(t.Context()).Scan(&count); err != nil || count != 1 {
		t.Fatal("statement/pool did not recover after restart", count, err)
	}
}
