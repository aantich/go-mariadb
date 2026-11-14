// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

// Stage 4 workloads are additive: the original 200 profiles are untouched.
// Setup/retained prepare/close are outside timing. One-shot helpers still time
// their own prepare/close. Every write alternative includes the same transaction,
// deletion and content verification. None of these functions runs in go test's
// ordinary correctness mode; first measurements require an authorized idle run.
func stage4BenchmarkConn(b *testing.B, options ...mariadb.Option) *sql.Conn {
	b.Helper()
	cfg := driverConfig(b, "text")
	if err := cfg.Apply(options...); err != nil {
		b.Fatal(err)
	}
	db := openConfig(b, cfg)
	conn, err := db.Conn(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	return conn
}

func stage4BenchmarkTable(b *testing.B, conn *sql.Conn, name, columns string) {
	b.Helper()
	// Fixed names are confined to the reserved schema and serial benchmark
	// family. Use durable InnoDB tables for writes/DDL, as in the original suite.
	mustExec(b, conn, "DROP TABLE IF EXISTS "+name)
	mustExec(b, conn, "CREATE TABLE "+name+"("+columns+") ENGINE=InnoDB")
	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			b.Errorf("reserved benchmark table cleanup: %v", err)
		}
	})
}

func BenchmarkMariaDBStage4Bulk(b *testing.B) {
	for _, count := range []int{1, 16, 100, 1000} {
		for _, method := range []string{"aggregate_once", "aggregate_reused", "units_once", "units_reused", "prepared_loop"} {
			b.Run(fmt.Sprintf("%d/%s", count, method), func(b *testing.B) {
				conn := stage4BenchmarkConn(b, mariadb.WithBulkUnitResults(true))
				stage4BenchmarkTable(b, conn, "go_stage4_bench", "id INT PRIMARY KEY,v VARCHAR(40)")
				const query = "INSERT INTO go_stage4_bench VALUES (?,?)"
				native, err := mariadb.PrepareNative(b.Context(), conn, query)
				if err != nil {
					b.Fatal(err)
				}
				defer native.Close()
				ordinary, err := conn.PrepareContext(b.Context(), query)
				if err != nil {
					b.Fatal(err)
				}
				defer ordinary.Close()
				rows := make([][]any, count)
				for i := range rows {
					rows[i] = []any{int64(i), "bulk payload"}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					mustExec(b, conn, "BEGIN")
					mustExec(b, conn, "DELETE FROM go_stage4_bench")
					var units []mariadb.BulkUnitResult
					switch method {
					case "aggregate_once":
						_, err = mariadb.BulkExec(b.Context(), conn, query, rows)
					case "aggregate_reused":
						_, err = native.BulkExec(b.Context(), rows)
					case "units_once":
						units, err = mariadb.BulkExecUnits(b.Context(), conn, query, rows)
					case "units_reused":
						units, err = native.BulkExecUnits(b.Context(), rows)
					case "prepared_loop":
						for _, row := range rows {
							if _, err = ordinary.ExecContext(b.Context(), row...); err != nil {
								b.Fatal(err)
							}
						}
					}
					if err != nil {
						b.Fatal(err)
					}
					if method == "units_once" || method == "units_reused" {
						if err := stage4CheckUnits(units, count); err != nil {
							b.Fatal(err)
						}
					}
					mustExec(b, conn, "COMMIT")
					var got, sum int
					if err := conn.QueryRowContext(b.Context(), "SELECT COUNT(*),COALESCE(SUM(id),0) FROM go_stage4_bench WHERE v='bulk payload'").Scan(&got, &sum); err != nil || got != count || sum != count*(count-1)/2 {
						b.Fatalf("count=%d sum=%d error=%v", got, sum, err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(count), "rows/op")
			})
		}
	}
}

// The selected unit-results API determines the contract. A nil slice must not
// silently turn the unit benchmark into an aggregate-only measurement.
func stage4CheckUnits(units []mariadb.BulkUnitResult, count int) error {
	if len(units) != count {
		return fmt.Errorf("unit count=%d want=%d", len(units), count)
	}
	for _, unit := range units {
		if unit.RowsAffected != 1 {
			return errors.New("incorrect unit counter")
		}
	}
	return nil
}

// Generate fresh bytes for every upload. Reader cases do not retain an entire
// payload; byte-slice cases materialize exactly once using the same generator.
// Generation and verification costs are part of this application-level workload.
type stage4PatternReader struct{ remaining int }

func (r *stage4PatternReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	for i := range p[:n] {
		p[i] = 0xa7
	}
	r.remaining -= n
	return n, nil
}

func stage4Digest(size int) string {
	hash := md5.New()
	io.Copy(hash, &stage4PatternReader{remaining: size})
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func BenchmarkMariaDBStage4Stream(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20, 16 << 20} {
		for _, method := range []string{"bytes_once", "bytes_reused", "bytes_sql_stmt", "reader_once", "reader_reused"} {
			b.Run(fmt.Sprintf("%d/%s", size, method), func(b *testing.B) {
				conn := stage4BenchmarkConn(b)
				stage4BenchmarkTable(b, conn, "go_stage4_blob", "b LONGBLOB")
				const query = "INSERT INTO go_stage4_blob VALUES (?)"
				native, err := mariadb.PrepareNative(b.Context(), conn, query)
				if err != nil {
					b.Fatal(err)
				}
				defer native.Close()
				ordinary, err := conn.PrepareContext(b.Context(), query)
				if err != nil {
					b.Fatal(err)
				}
				defer ordinary.Close()
				expected := stage4Digest(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					mustExec(b, conn, "BEGIN")
					mustExec(b, conn, "DELETE FROM go_stage4_blob")
					reader := &stage4PatternReader{remaining: size}
					switch method {
					case "reader_once":
						_, err = mariadb.ExecStream(b.Context(), conn, query, mariadb.StreamParam{Reader: reader, MaxBytes: int64(size)})
					case "reader_reused":
						_, err = native.ExecStream(b.Context(), mariadb.StreamParam{Reader: reader, MaxBytes: int64(size)})
					default:
						payload := make([]byte, size)
						if _, err := io.ReadFull(reader, payload); err != nil {
							b.Fatal(err)
						}
						switch method {
						case "bytes_once":
							_, err = conn.ExecContext(b.Context(), query, payload)
						case "bytes_reused":
							_, err = native.ExecStream(b.Context(), payload)
						case "bytes_sql_stmt":
							_, err = ordinary.ExecContext(b.Context(), payload)
						}
					}
					if err != nil {
						b.Fatal(err)
					}
					mustExec(b, conn, "COMMIT")
					var count, length int
					var digest string
					if err := conn.QueryRowContext(b.Context(), "SELECT COUNT(*),MAX(OCTET_LENGTH(b)),MAX(MD5(b)) FROM go_stage4_blob").Scan(&count, &length, &digest); err != nil || count != 1 || length != size || digest != expected {
						b.Fatalf("count=%d length=%d digest=%s error=%v", count, length, digest, err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(size), "payload-B/op")
			})
		}
	}
}

func BenchmarkMariaDBStage4Cursor(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		for _, fetch := range []uint32{0, 1, 16, 128, 1024} {
			for _, scan := range []string{"typed", "borrowed"} {
				b.Run(fmt.Sprintf("%d/%d/%s", count, fetch, scan), func(b *testing.B) {
					options := []mariadb.Option{mariadb.WithBulkOperations(true)}
					if fetch > 0 {
						options = append(options, mariadb.WithServerCursors(fetch))
					}
					conn := stage4BenchmarkConn(b, options...)
					mustExec(b, conn, "CREATE TEMPORARY TABLE go_stage4_cursor(id INT PRIMARY KEY,v VARCHAR(40))")
					data := make([][]any, count)
					for i := range data {
						data[i] = []any{int64(i), "cursor payload"}
					}
					if _, err := mariadb.BulkExec(b.Context(), conn, "INSERT INTO go_stage4_cursor VALUES (?,?)", data); err != nil {
						b.Fatal(err)
					}
					stmt, err := conn.PrepareContext(b.Context(), "SELECT id,v FROM go_stage4_cursor ORDER BY id")
					if err != nil {
						b.Fatal(err)
					}
					defer stmt.Close()
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						rows, err := stmt.QueryContext(b.Context())
						if err != nil {
							b.Fatal(err)
						}
						seen := 0
						for rows.Next() {
							var id int
							var value string
							if scan == "typed" {
								err = rows.Scan(&id, &value)
							} else {
								var rawID, rawValue sql.RawBytes
								if err = rows.Scan(&rawID, &rawValue); err == nil {
									id, err = strconv.Atoi(string(rawID))
									if !bytes.Equal(rawValue, []byte("cursor payload")) {
										b.Fatal("incorrect borrowed value")
									}
									value = "cursor payload"
								}
							}
							if err != nil || id != seen || value != "cursor payload" {
								b.Fatalf("row=%d id=%d error=%v", seen, id, err)
							}
							seen++
						}
						if err := rows.Err(); err != nil {
							b.Fatal(err)
						}
						if err := rows.Close(); err != nil || seen != count {
							b.Fatalf("seen=%d error=%v", seen, err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(count), "rows/op")
				})
			}
		}
	}
}

func BenchmarkMariaDBStage4Progress(b *testing.B) {
	for _, method := range []string{"disabled", "discarded", "callback", "callback_error"} {
		b.Run(method, func(b *testing.B) {
			conn := stage4BenchmarkConn(b, mariadb.WithProgressReports(method != "disabled"))
			stage4BenchmarkTable(b, conn, "go_stage4_progress", "v INT")
			mustExec(b, conn, "INSERT INTO go_stage4_progress SELECT seq FROM seq_1_to_1000")
			marker := errors.New("stop observing")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, query := range []string{"ALTER TABLE go_stage4_progress ADD extra INT, ALGORITHM=COPY", "ALTER TABLE go_stage4_progress DROP extra, ALGORITHM=COPY"} {
					var err error
					if method == "disabled" || method == "discarded" {
						_, err = conn.ExecContext(b.Context(), query)
					} else {
						reports := 0
						result, progressErr := mariadb.ExecWithProgress(b.Context(), conn, func(mariadb.Progress) error {
							reports++
							if method == "callback_error" {
								return marker
							}
							return nil
						}, query)
						err = progressErr
						if reports == 0 {
							b.Fatal("required ALTER progress fixture emitted no reports")
						}
						if method == "callback_error" && result != nil && b.Context().Err() == nil && errors.Is(err, marker) {
							err = nil // Observation failed, but the SQL response was drained.
						}
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				var count, sum int
				if err := conn.QueryRowContext(b.Context(), "SELECT COUNT(*),SUM(v) FROM go_stage4_progress").Scan(&count, &sum); err != nil || count != 1000 || sum != 500500 {
					b.Fatalf("count=%d sum=%d error=%v", count, sum, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(2, "commands/op")
		})
	}
}

func BenchmarkMariaDBStage4ProgressQuiet(b *testing.B) {
	for _, method := range []string{"disabled", "discarded", "callback"} {
		b.Run(method, func(b *testing.B) {
			conn := stage4BenchmarkConn(b, mariadb.WithProgressReports(method != "disabled"))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var err error
				if method == "callback" {
					_, err = mariadb.ExecWithProgress(b.Context(), conn, func(mariadb.Progress) error { return nil }, "DO 1")
				} else {
					_, err = conn.ExecContext(b.Context(), "DO 1")
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(1, "commands/op")
		})
	}
}
