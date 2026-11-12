package validation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	mysql "github.com/aantich/go-mariadb"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Values are checked through the public API on every supported transport/path.
// Binary preparation is explicit, including queries with no bound parameters.
func TestMariaDBValues(t *testing.T) {
	cases := []struct{ name, expr, want string }{
		{"signed_min", "CAST(-9223372036854775808 AS SIGNED)", "-9223372036854775808"},
		{"signed_max", "CAST(9223372036854775807 AS SIGNED)", "9223372036854775807"},
		{"unsigned_max", "CAST(18446744073709551615 AS UNSIGNED)", "18446744073709551615"},
		{"negative", "-42", "-42"}, {"zero", "0", "0"},
		{"decimal_exact", "CAST('123456789012345678901234567890.123456789' AS DECIMAL(50,9))", "123456789012345678901234567890.123456789"},
		{"decimal_negative", "CAST('-0.000000001' AS DECIMAL(20,9))", "-0.000000001"},
		{"double", "CAST('1.25' AS DOUBLE)", "1.25"}, {"empty", "''", ""}, {"ascii", "'MariaDB'", "MariaDB"},
		{"unicode", "CONVERT(0x48656c6c6f20f09f8c8d USING utf8mb4)", "Hello 🌍"},
		{"nul_bytes", "0x610062", "a\x00b"}, {"binary_high", "0x00ff80", "\x00\xff\x80"},
		{"date", "CAST('2024-02-29' AS DATE)", "2024-02-29"},
		{"zero_date", "CAST('0000-00-00' AS DATE)", "0000-00-00"},
		{"datetime_microseconds", "CAST('2024-02-29 12:34:56.123456' AS DATETIME(6))", "2024-02-29 12:34:56.123456"},
		{"negative_time", "CAST('-838:59:59.123456' AS TIME(6))", "-838:59:59.123456"},
		{"json", "JSON_COMPACT(JSON_OBJECT('a',1,'b',JSON_ARRAY(2,3)))", `{"a":1,"b":[2,3]}`},
		{"uuid", "CAST('123e4567-e89b-12d3-a456-426614174000' AS UUID)", "123e4567-e89b-12d3-a456-426614174000"},
		{"inet4", "CAST('192.0.2.7' AS INET4)", "192.0.2.7"},
		{"inet6", "CAST('2001:db8::1' AS INET6)", "2001:db8::1"},
		{"vector", "VEC_ToText(VEC_FromText('[1,2,3]'))", "[1,2,3]"},
		{"geometry", "ST_AsText(ST_GeomFromText('POINT(1 2)'))", "POINT(1 2)"},
		{"bit", "b'10000001'", "\x81"},
	}
	for _, mode := range []string{"text", "prepared", "interpolated", "compressed", "socket", "tls"} {
		t.Run(mode, func(t *testing.T) {
			db := openDB(t, mode)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					query := "SELECT " + tc.expr
					var row *sql.Row
					if mode == "prepared" || mode == "compressed" {
						stmt, err := db.PrepareContext(t.Context(), query)
						if err != nil {
							t.Fatal(err)
						}
						defer stmt.Close()
						row = stmt.QueryRowContext(t.Context())
					} else {
						row = db.QueryRowContext(t.Context(), query)
					}
					var got string
					if err := row.Scan(&got); err != nil {
						t.Fatal(err)
					}
					if got != tc.want {
						t.Fatalf("got %q, want %q", got, tc.want)
					}
				})
			}
			t.Run("null_vs_empty", func(t *testing.T) {
				var a, b []byte
				if err := db.QueryRow("SELECT NULL,''").Scan(&a, &b); err != nil {
					t.Fatal(err)
				}
				if a != nil || b == nil || len(b) != 0 {
					t.Fatalf("NULL/empty conflated: %#v %#v", a, b)
				}
			})
		})
	}
}

func TestMariaDBBindings(t *testing.T) {
	for _, mode := range []string{"prepared", "interpolated", "compressed"} {
		t.Run(mode, func(t *testing.T) {
			db := openDB(t, mode)
			for _, value := range []string{"", "quote'back\\slash\x00\n\r\x1a", "🌍 français 日本語", "x'); DROP TABLE forbidden; -- "} {
				t.Run(fmt.Sprintf("bytes_%d", len(value)), func(t *testing.T) { scalar(t, db, "SELECT ?", value, value) })
			}
			t.Run("unsigned_parameter", func(t *testing.T) { scalar(t, db, "SELECT ?", "18446744073709551615", uint64(math.MaxUint64)) })
			t.Run("large_parameter", func(t *testing.T) {
				payload := bytes.Repeat([]byte("abcdefgh"), 256*1024)
				var n int
				var same bool
				if err := db.QueryRow("SELECT OCTET_LENGTH(?), SHA2(?,256)=SHA2(REPEAT('abcdefgh',262144),256)", payload, payload).Scan(&n, &same); err != nil {
					t.Fatal(err)
				}
				if n != len(payload) || !same {
					t.Fatalf("long-data mismatch: %d %v", n, same)
				}
			})
			t.Run("no_backslash_escapes", func(t *testing.T) {
				conn, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				mustExec(t, conn, "SET SESSION sql_mode='NO_BACKSLASH_ESCAPES'")
				defer conn.ExecContext(context.Background(), "SET SESSION sql_mode=DEFAULT")
				want := "slash\\quote'🌍\x00"
				var got string
				if err := conn.QueryRowContext(t.Context(), "SELECT ?", want).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("binding corruption: %q", got)
				}
			})
		})
	}
}

func TestMariaDBSQL(t *testing.T) {
	db := openDB(t, "text")
	cases := []struct{ name, query, want string }{
		{"cte", "WITH c AS (SELECT 7 AS n) SELECT n FROM c", "7"},
		{"recursive_cte", "WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM c WHERE n<5) SELECT SUM(n) FROM c", "15"},
		{"window", "SELECT SUM(r) FROM (SELECT ROW_NUMBER() OVER(ORDER BY seq) r FROM seq_1_to_5) x", "15"},
		{"json_table", `SELECT SUM(n) FROM JSON_TABLE('[1,2,3]', '$[*]' COLUMNS(n INT PATH '$')) j`, "6"},
		{"sequence_engine", "SELECT COUNT(*) FROM seq_1_to_1000", "1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { scalar(t, db, tc.query, tc.want) })
	}
	t.Run("insert_returning", func(t *testing.T) {
		mustExec(t, db, "CREATE TEMPORARY TABLE go_returning(id INT PRIMARY KEY AUTO_INCREMENT,v INT)")
		scalar(t, db, "INSERT INTO go_returning(v) VALUES (17) RETURNING id", "1")
	})
	t.Run("sequence", func(t *testing.T) {
		mustExec(t, db, "DROP SEQUENCE IF EXISTS go_sequence")
		mustExec(t, db, "CREATE SEQUENCE go_sequence START WITH 21")
		defer db.Exec("DROP SEQUENCE go_sequence")
		scalar(t, db, "SELECT NEXT VALUE FOR go_sequence", "21")
	})
	t.Run("system_versioning", func(t *testing.T) {
		mustExec(t, db, "DROP TABLE IF EXISTS go_versioned")
		mustExec(t, db, "CREATE TABLE go_versioned(id INT PRIMARY KEY,v INT) WITH SYSTEM VERSIONING")
		defer db.Exec("DROP TABLE go_versioned")
		mustExec(t, db, "INSERT INTO go_versioned VALUES (1,10)")
		mustExec(t, db, "UPDATE go_versioned SET v=20 WHERE id=1")
		scalar(t, db, "SELECT COUNT(*) FROM go_versioned FOR SYSTEM_TIME ALL", "2")
	})
	t.Run("oracle_mode", func(t *testing.T) {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		mustExec(t, conn, "SET SESSION sql_mode='ORACLE'")
		defer conn.ExecContext(context.Background(), "SET SESSION sql_mode=DEFAULT")
		var n int
		if err := conn.QueryRowContext(t.Context(), "SELECT NVL(NULL,42) FROM DUAL").Scan(&n); err != nil || n != 42 {
			t.Fatalf("Oracle mode: %d %v", n, err)
		}
	})
}

func TestMariaDBResults(t *testing.T) {
	t.Run("multiple_text_results", func(t *testing.T) {
		cfg := driverConfig(t, "text")
		cfg.MultiStatements = true
		db := openConfig(t, cfg)
		rows, err := db.Query("SELECT 1; SELECT 'second'; SELECT 3")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for {
			for rows.Next() {
				var x string
				if err := rows.Scan(&x); err != nil {
					t.Fatal(err)
				}
				got = append(got, x)
			}
			if !rows.NextResultSet() {
				break
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"1", "second", "3"}) {
			t.Fatalf("results=%v", got)
		}
	})
	t.Run("prepared_procedure_repeat", func(t *testing.T) {
		db := openDB(t, "prepared")
		mustExec(t, db, "DROP PROCEDURE IF EXISTS go_results")
		mustExec(t, db, "CREATE PROCEDURE go_results(IN n INT) BEGIN SELECT n AS a; SELECT CONCAT('v',n) AS b,n+1 AS c; END")
		defer db.Exec("DROP PROCEDURE go_results")
		stmt, err := db.Prepare("CALL go_results(?)")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		for i := 1; i <= 3; i++ {
			rows, err := stmt.Query(i)
			if err != nil {
				t.Fatal(err)
			}
			sets := 0
			for {
				columns, err := rows.Columns()
				if err != nil {
					t.Fatal(err)
				}
				values := make([]any, len(columns))
				dest := make([]any, len(columns))
				for j := range values {
					dest[j] = &values[j]
				}
				for rows.Next() {
					if err := rows.Scan(dest...); err != nil {
						t.Fatal(err)
					}
				}
				sets++
				if !rows.NextResultSet() {
					break
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if sets != 2 {
				t.Fatalf("got %d result sets", sets)
			}
		}
	})
	t.Run("metadata_after_ddl", func(t *testing.T) {
		db := openDB(t, "prepared")
		mustExec(t, db, "DROP TABLE IF EXISTS go_metadata")
		mustExec(t, db, "CREATE TABLE go_metadata(a INT)")
		defer db.Exec("DROP TABLE go_metadata")
		stmt, err := db.Prepare("SELECT * FROM go_metadata")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		for i := 0; i < 3; i++ {
			rows, err := stmt.Query()
			if err != nil {
				t.Fatal(err)
			}
			cols, err := rows.Columns()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(cols) != i+1 {
				t.Fatalf("stale metadata: %v", cols)
			}
			if i < 2 {
				mustExec(t, db, fmt.Sprintf("ALTER TABLE go_metadata ADD c%d VARCHAR(10)", i))
			}
		}
	})
	t.Run("unsigned_insert_id_contract", func(t *testing.T) {
		db := openDB(t, "text")
		mustExec(t, db, "DROP TABLE IF EXISTS go_unsigned_id")
		mustExec(t, db, "CREATE TABLE go_unsigned_id(id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT) AUTO_INCREMENT=9223372036854775808")
		defer db.Exec("DROP TABLE go_unsigned_id")
		r := mustExec(t, db, "INSERT INTO go_unsigned_id VALUES (NULL)")
		id, err := r.LastInsertId()
		if err == nil && id < 0 {
			t.Fatalf("unsigned insert ID silently wrapped to %d", id)
		}
	})
}

func TestMariaDBTransactions(t *testing.T) {
	db := openDB(t, "text")
	mustExec(t, db, "DROP TABLE IF EXISTS go_tx")
	mustExec(t, db, "CREATE TABLE go_tx(id INT PRIMARY KEY,v INT) ENGINE=InnoDB")
	defer db.Exec("DROP TABLE go_tx")
	t.Run("rollback", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, tx, "INSERT INTO go_tx VALUES (1,10)")
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		scalar(t, db, "SELECT COUNT(*) FROM go_tx", "0")
	})
	t.Run("commit", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, tx, "INSERT INTO go_tx VALUES (1,10)")
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		scalar(t, db, "SELECT v FROM go_tx WHERE id=1", "10")
	})
	t.Run("savepoint", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		mustExec(t, tx, "SAVEPOINT s")
		mustExec(t, tx, "UPDATE go_tx SET v=99")
		mustExec(t, tx, "ROLLBACK TO s")
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		scalar(t, db, "SELECT v FROM go_tx WHERE id=1", "10")
	})
	t.Run("read_only", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec("UPDATE go_tx SET v=20"); err == nil {
			t.Fatal("write in read-only transaction succeeded")
		}
	})
	for _, level := range []sql.IsolationLevel{sql.LevelReadCommitted, sql.LevelRepeatableRead, sql.LevelSerializable} {
		t.Run(level.String(), func(t *testing.T) {
			tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: level})
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMariaDBReliability(t *testing.T) {
	t.Run("cancel_then_fresh_connection", func(t *testing.T) {
		db := openDB(t, "text")
		db.SetMaxOpenConns(1)
		var before, after int
		if err := db.QueryRow("SELECT CONNECTION_ID()").Scan(&before); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		var x int
		err := db.QueryRowContext(ctx, "SELECT SLEEP(3)").Scan(&x)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation=%v", err)
		}
		if err := db.QueryRow("SELECT CONNECTION_ID()").Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before == after {
			t.Fatal("cancelled protocol connection was reused")
		}
	})
	t.Run("early_close_drain", func(t *testing.T) {
		db := openDB(t, "prepared")
		db.SetMaxOpenConns(1)
		rows, err := db.Query("SELECT seq FROM seq_1_to_1000")
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("missing first row")
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		scalar(t, db, "SELECT 42", "42")
	})
	t.Run("bounded_pool_concurrency", func(t *testing.T) {
		db := openDB(t, "text")
		db.SetMaxOpenConns(4)
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					var got int
					if err := db.QueryRowContext(t.Context(), "SELECT ?", n).Scan(&got); err != nil {
						errs <- err
						return
					}
					if got != n {
						errs <- fmt.Errorf("got %d want %d", got, n)
						return
					}
				}
			}(worker)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if db.Stats().OpenConnections > 4 {
			t.Fatal("pool connection limit exceeded")
		}
	})
	t.Run("server_error_identity", func(t *testing.T) {
		db := openDB(t, "text")
		_, err := db.Exec("SELECT missing_column FROM (SELECT 1 AS a) q")
		var e *mysql.MySQLError
		if !errors.As(err, &e) || e.Number != 1054 || string(e.SQLState[:]) != "42S22" {
			t.Fatalf("server error=%v", err)
		}
		scalar(t, db, "SELECT 42", "42")
	})
	t.Run("owned_bytes", func(t *testing.T) {
		db := openDB(t, "text")
		rows, err := db.Query("SELECT 'first' UNION ALL SELECT 'other'")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal("missing row")
		}
		var owned []byte
		if err := rows.Scan(&owned); err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("missing second row")
		}
		var next sql.RawBytes
		if err := rows.Scan(&next); err != nil {
			t.Fatal(err)
		}
		if string(owned) != "first" {
			t.Fatal("owned []byte changed after Next")
		}
	})
}

// This is a desired opt-in isolation contract, not an existing database/sql
// guarantee. Its failure is reported as a capability gap, not a regression.
func TestMariaDBDesiredCleanPoolReset(t *testing.T) {
	db := openDB(t, "text")
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, "SET @go_private_request='secret'")
	conn.Close()
	var leaked sql.NullString
	if err := db.QueryRow("SELECT @go_private_request").Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked.Valid {
		t.Fatal("GAP: pool checkout retains previous request user variables; ResetSession is liveness-only")
	}
}

func TestMariaDBAuthentication(t *testing.T) {
	for _, mode := range []string{"text", "socket", "tls", "x509"} {
		t.Run(mode, func(t *testing.T) { db := openDB(t, mode); scalar(t, db, "SELECT 1", "1") })
	}
	for _, plugin := range []string{"ed25519", "parsec", "caching_sha2_password"} {
		t.Run(plugin, func(t *testing.T) {
			c := fixtures(t)
			if !slices.Contains(c.Plugins, plugin) {
				t.Skip("ENVIRONMENT: server auth plugin not installed")
			}
			cfg := driverConfig(t, "tls")
			cfg.User = "gomdb_" + plugin
			db := openConfig(t, cfg)
			scalar(t, db, "SELECT 1", "1")
		})
	}
	t.Run("unix_socket_identity", func(t *testing.T) {
		cfg := driverConfig(t, "socket")
		cfg.User = "gomdb_socket"
		cfg.Passwd = ""
		db := openConfig(t, cfg)
		scalar(t, db, "SELECT CURRENT_USER()", "gomdb_socket@localhost")
	})
	t.Run("wrong_password_rejected", func(t *testing.T) {
		cfg := driverConfig(t, "text")
		cfg.Passwd = "incorrect"
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatal(err)
		}
		db := sql.OpenDB(connector)
		defer db.Close()
		if err := db.PingContext(t.Context()); err == nil {
			t.Fatal("wrong password accepted")
		}
	})
	t.Run("tls_hostname_rejected", func(t *testing.T) {
		cfg := driverConfig(t, "tls")
		cfg.TLS.ServerName = "wrong.invalid"
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatal(err)
		}
		db := sql.OpenDB(connector)
		defer db.Close()
		if err := db.PingContext(t.Context()); err == nil {
			t.Fatal("wrong TLS hostname accepted")
		}
	})
}

func TestMariaDBLocalInfile(t *testing.T) {
	db := openDB(t, "text")
	mustExec(t, db, "DROP TABLE IF EXISTS go_infile")
	mustExec(t, db, "CREATE TABLE go_infile(id INT,v VARCHAR(20))")
	defer db.Exec("DROP TABLE go_infile")
	mysql.RegisterReaderHandler("go_validation", func() io.Reader { return strings.NewReader("1\tfirst\n2\tsecond\n") })
	defer mysql.DeregisterReaderHandler("go_validation")
	mustExec(t, db, "LOAD DATA LOCAL INFILE 'Reader::go_validation' INTO TABLE go_infile")
	scalar(t, db, "SELECT CONCAT(COUNT(*),':',SUM(id)) FROM go_infile", "2:3")
	t.Run("unregistered_reader_rejected", func(t *testing.T) {
		if _, err := db.Exec("LOAD DATA LOCAL INFILE 'Reader::not_registered' INTO TABLE go_infile"); err == nil {
			t.Fatal("unregistered reader accepted")
		}
	})
}
