package validation

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	mysql "github.com/aantich/go-mariadb"
)

// Each read operation consumes and validates every row. Averages include
// database/sql overhead, transfer and decoding; they are not decoder-only times.
func BenchmarkMariaDBRead(b *testing.B) {
	for _, mode := range []string{"text", "prepared", "compressed", "socket", "tls"} {
		for _, shape := range []string{"narrow", "wide", "blob"} {
			for _, raw := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/%s/raw=%t", mode, shape, raw), func(b *testing.B) {
					db := openDB(b, mode)
					columns := []string{"seq"}
					wantRows := 1000
					wantSum := int64(500500)
					query := "SELECT seq,CONCAT('v',seq) FROM seq_1_to_1000"
					columns = append(columns, "label")
					if shape == "wide" {
						columns = columns[:0]
						for i := 0; i < 32; i++ {
							columns = append(columns, fmt.Sprintf("seq AS c%d", i))
						}
						query = "SELECT " + strings.Join(columns, ",") + " FROM seq_1_to_1000"
					}
					if shape == "blob" {
						query = "SELECT REPEAT('x',1048576)"
						columns = []string{"blob"}
						wantRows = 1
						wantSum = 1048576
						b.SetBytes(1048576)
					}
					var stmt *sql.Stmt
					// Text/socket/TLS keep COM_QUERY; prepared/compressed isolate binary rows.
					if mode == "prepared" || mode == "compressed" {
						var err error
						stmt, err = db.Prepare(query)
						if err != nil {
							b.Fatal(err)
						}
						defer stmt.Close()
					}
					run := func() {
						var rows *sql.Rows
						var err error
						if stmt != nil {
							rows, err = stmt.Query()
						} else {
							rows, err = db.Query(query)
						}
						if err != nil {
							b.Fatal(err)
						}
						n := 0
						sum := int64(0)
						if raw {
							values := make([]sql.RawBytes, len(columns))
							dest := make([]any, len(columns))
							for i := range values {
								dest[i] = &values[i]
							}
							for rows.Next() {
								if err := rows.Scan(dest...); err != nil {
									b.Fatal(err)
								}
								n++
								if shape == "blob" {
									if len(values[0]) != 1048576 || bytes.Count(values[0], []byte{'x'}) != 1048576 {
										b.Fatal("blob content mismatch")
									}
									sum += int64(len(values[0]))
								} else {
									v, err := strconv.ParseInt(string(values[0]), 10, 64)
									if err != nil {
										b.Fatal(err)
									}
									sum += v
									for i := 1; i < len(values); i++ {
										if shape == "wide" && string(values[i]) != string(values[0]) {
											b.Fatal("wide column mismatch")
										}
									}
									if shape == "narrow" && string(values[1]) != "v"+strconv.FormatInt(v, 10) {
										b.Fatal("label mismatch")
									}
								}
							}
						} else {
							values := make([]int64, len(columns))
							dest := make([]any, len(columns))
							for i := range values {
								dest[i] = &values[i]
							}
							for rows.Next() {
								if shape == "narrow" {
									var label string
									if err := rows.Scan(&values[0], &label); err != nil {
										b.Fatal(err)
									}
									if label != "v"+strconv.FormatInt(values[0], 10) {
										b.Fatal("label mismatch")
									}
								} else if shape == "blob" {
									var payload []byte
									if err := rows.Scan(&payload); err != nil {
										b.Fatal(err)
									}
									if len(payload) != 1048576 || bytes.Count(payload, []byte{'x'}) != 1048576 {
										b.Fatal("blob content mismatch")
									}
									values[0] = int64(len(payload))
								} else {
									if err := rows.Scan(dest...); err != nil {
										b.Fatal(err)
									}
									for _, v := range values[1:] {
										if v != values[0] {
											b.Fatal("wide column mismatch")
										}
									}
								}
								n++
								sum += values[0]
							}
						}
						err = rows.Err()
						closeErr := rows.Close()
						if err != nil {
							b.Fatal(err)
						}
						if closeErr != nil {
							b.Fatal(closeErr)
						}
						if n != wantRows || sum != wantSum {
							b.Fatalf("row/checksum mismatch: %d %d", n, sum)
						}
					}
					run()
					run() // Same two explicit validation/warmup operations for every path.
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						run()
					}
					b.StopTimer()
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*wantRows), "ns/row")
				})
			}
		}
	}
}

// These alternatives perform identical 100-row transactional replacements.
// BEGIN, DELETE, inserts, COMMIT and final count/sum validation are all timed.
// Native MariaDB bulk is intentionally absent until the driver exposes it.
func BenchmarkMariaDBWrite(b *testing.B) {
	for _, kind := range []string{"sequential_prepared_100", "text_batch_100", "load_data_reader_100"} {
		b.Run(kind, func(b *testing.B) {
			db := openDB(b, "text")
			mustExec(b, db, "DROP TABLE IF EXISTS go_bench_write")
			mustExec(b, db, "CREATE TABLE go_bench_write(id INT PRIMARY KEY,v VARCHAR(24)) ENGINE=InnoDB")
			defer db.Exec("DROP TABLE go_bench_write")
			stmt, err := db.Prepare("INSERT INTO go_bench_write VALUES (?,?)")
			if err != nil {
				b.Fatal(err)
			}
			defer stmt.Close()
			var tuples []string
			var data strings.Builder
			for i := 1; i <= 100; i++ {
				tuples = append(tuples, fmt.Sprintf("(%d,'v%d')", i, i))
				fmt.Fprintf(&data, "%d\tv%d\n", i, i)
			}
			batch := "INSERT INTO go_bench_write VALUES " + strings.Join(tuples, ",")
			mysql.RegisterReaderHandler("go_bench", func() io.Reader { return strings.NewReader(data.String()) })
			defer mysql.DeregisterReaderHandler("go_bench")
			run := func() {
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				if _, err = tx.Exec("DELETE FROM go_bench_write"); err != nil {
					tx.Rollback()
					b.Fatal(err)
				}
				switch kind {
				case "sequential_prepared_100":
					s := tx.Stmt(stmt)
					for i := 1; i <= 100; i++ {
						if _, err = s.Exec(i, fmt.Sprintf("v%d", i)); err != nil {
							break
						}
					}
					s.Close()
				case "text_batch_100":
					_, err = tx.Exec(batch)
				case "load_data_reader_100":
					_, err = tx.Exec("LOAD DATA LOCAL INFILE 'Reader::go_bench' INTO TABLE go_bench_write")
				}
				if err != nil {
					tx.Rollback()
					b.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					b.Fatal(err)
				}
				var count, sum int
				if err = db.QueryRow("SELECT COUNT(*),SUM(id) FROM go_bench_write WHERE v=CONCAT('v',id)").Scan(&count, &sum); err != nil {
					b.Fatal(err)
				}
				if count != 100 || sum != 5050 {
					b.Fatalf("write verification: %d %d", count, sum)
				}
			}
			run()
			run()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*100), "ns/row")
		})
	}
}

func BenchmarkMariaDBConnect(b *testing.B) {
	for _, mode := range []string{"text", "socket", "tls"} {
		b.Run(mode, func(b *testing.B) {
			cfg := driverConfig(b, mode)
			connector, err := mysql.NewConnector(cfg)
			if err != nil {
				b.Fatal(err)
			}
			run := func() {
				conn, err := connector.Connect(context.Background())
				if err != nil {
					b.Fatal(err)
				}
				if err := conn.Close(); err != nil {
					b.Fatal(err)
				}
			}
			run()
			run()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
		})
	}
}

func BenchmarkMariaDBPool(b *testing.B) {
	for _, size := range []int{1, 8} {
		b.Run(fmt.Sprintf("connections_%d", size), func(b *testing.B) {
			db := openDB(b, "text")
			db.SetMaxOpenConns(size)
			db.SetMaxIdleConns(size)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var n int
					if err := db.QueryRow("SELECT 42").Scan(&n); err != nil {
						b.Error(err)
						return
					}
					if n != 42 {
						b.Error("wrong scalar result")
						return
					}
				}
			})
		})
	}
}
