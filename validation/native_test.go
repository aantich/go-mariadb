package validation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

func TestMariaDBNativeBulk(t *testing.T) {
	cfg := driverConfig(t, "text")
	if err := cfg.Apply(mariadb.WithBulkOperations(true)); err != nil {
		t.Fatal(err)
	}
	db := openConfig(t, cfg)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mustExec(t, conn, "DROP TABLE IF EXISTS go_native_bulk")
	mustExec(t, conn, "CREATE TABLE go_native_bulk(id BIGINT UNSIGNED PRIMARY KEY, s VARCHAR(100) DEFAULT 'default', b BLOB, n DOUBLE, d DATETIME(6))")
	defer conn.ExecContext(context.Background(), "DROP TABLE go_native_bulk")
	date := time.Date(2026, 11, 11, 12, 13, 14, 123456000, time.UTC)
	for _, count := range []int{1, 16, 100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			mustExec(t, conn, "TRUNCATE go_native_bulk")
			rows := make([][]any, count)
			for i := range rows {
				rows[i] = []any{uint64(i + 1), "hello", []byte{0, 1, 255}, 1.25, date}
			}
			rows[0][1] = mariadb.BulkDefault
			result, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk VALUES (?,?,?,?,?)", rows)
			if err != nil {
				t.Fatal(err)
			}
			if result.RowsAffectedUnsigned() != uint64(count) {
				t.Fatal(result.RowsAffectedUnsigned())
			}
			var got int
			var text string
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_native_bulk WHERE b=UNHEX('0001FF') AND n=1.25 AND d='2026-11-11 12:13:14.123456'").Scan(&got); err != nil || got != count {
				t.Fatalf("count=%d err=%v", got, err)
			}
			if err := conn.QueryRowContext(t.Context(), "SELECT s FROM go_native_bulk WHERE id=1").Scan(&text); err != nil || text != "default" {
				t.Fatalf("default=%s err=%v", text, err)
			}
		})
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk(id) VALUES (?)", [][]any{{uint64(math.MaxUint64)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk(id) VALUES (?)", [][]any{{uint64(math.MaxUint64)}}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk(id) VALUES (?)", [][]any{{uint64(5)}, {int64(6)}}); err == nil {
		t.Fatal("mixed signed types accepted")
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "UPDATE go_native_bulk SET s=? WHERE id=?", [][]any{{mariadb.BulkIgnore, uint64(1)}}); err != nil {
		t.Fatal("IGNORE:", err)
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk VALUES (?,?,?,?,?)", [][]any{{uint64(3000), nil, nil, nil, nil}}); err != nil {
		t.Fatal("NULL:", err)
	}
	if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_native_bulk(id) VALUES (?)", [][]any{{uint64(2000)}, {int64(2001)}}); err == nil {
		t.Fatal("inconsistent type accepted")
	}
	var partial int
	if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_native_bulk WHERE id=2000").Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("partial validation write: count=%d err=%v", partial, err)
	}
	if _, err := conn.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal("connection not reusable:", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mariadb.BulkExec(ctx, conn, "ignored", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMariaDBNativeUnsignedResult(t *testing.T) {
	db := openDB(t, "text")
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mustExec(t, conn, "DROP TABLE IF EXISTS go_native_unsigned")
	mustExec(t, conn, "CREATE TABLE go_native_unsigned(id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT, v INT) AUTO_INCREMENT=9223372036854775808")
	defer conn.ExecContext(context.Background(), "DROP TABLE go_native_unsigned")
	first, err := mariadb.ExecUnsigned(t.Context(), conn, "INSERT INTO go_native_unsigned(v) VALUES (?)", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.LastInsertIDUnsigned() != 1<<63 {
		t.Fatal(first.LastInsertIDUnsigned())
	}
	if _, err := first.LastInsertId(); !errors.Is(err, mariadb.ErrResultOverflow) {
		t.Fatal(err)
	}
	if _, err := mariadb.ExecUnsigned(t.Context(), conn, "INSERT INTO go_native_unsigned(v) VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	if first.LastInsertIDUnsigned() != 1<<63 {
		t.Fatal("result changed after next execution")
	}
}

func BenchmarkMariaDBNativeBulk(b *testing.B) {
	cfg := driverConfig(b, "text")
	cfg.Apply(mariadb.WithBulkOperations(true))
	db := openConfig(b, cfg)
	conn, err := db.Conn(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	mustExec(b, conn, "CREATE TABLE IF NOT EXISTS go_native_bench(id INT PRIMARY KEY, v VARCHAR(40))")
	defer conn.ExecContext(context.Background(), "DROP TABLE go_native_bench")
	for _, count := range []int{1, 16, 100, 1000} {
		rows := make([][]any, count)
		var tuples []string
		var tsv bytes.Buffer
		for i := range rows {
			rows[i] = []any{int64(i), "bulk payload"}
			tuples = append(tuples, fmt.Sprintf("(%d,'bulk payload')", i))
			fmt.Fprintf(&tsv, "%d\tbulk payload\n", i)
		}
		query := "INSERT INTO go_native_bench VALUES (?,?)"
		batch := "INSERT INTO go_native_bench VALUES " + strings.Join(tuples, ",")
		prepared, err := conn.PrepareContext(b.Context(), query)
		if err != nil {
			b.Fatal(err)
		}
		mariadb.RegisterReaderHandler("gomdb_native_bench", func() io.Reader { return bytes.NewReader(tsv.Bytes()) })
		for _, strategy := range []string{"prepared", "text_batch", "local_infile", "native_bulk"} {
			b.Run(fmt.Sprintf("%d/%s", count, strategy), func(b *testing.B) {
				// All alternatives include the same transaction, deletion and
				// count/sum/content verification. Native bulk includes transient
				// prepare/close; the ordinary prepared control reuses its handle.
				for i := 0; i < b.N; i++ {
					mustExec(b, conn, "BEGIN")
					mustExec(b, conn, "DELETE FROM go_native_bench")
					switch strategy {
					case "prepared":
						for _, row := range rows {
							if _, err := prepared.ExecContext(b.Context(), row...); err != nil {
								b.Fatal(err)
							}
						}
					case "text_batch":
						mustExec(b, conn, batch)
					case "local_infile":
						mustExec(b, conn, "LOAD DATA LOCAL INFILE 'Reader::gomdb_native_bench' INTO TABLE go_native_bench")
					case "native_bulk":
						if _, err := mariadb.BulkExec(b.Context(), conn, query, rows); err != nil {
							b.Fatal(err)
						}
					}
					mustExec(b, conn, "COMMIT")
					var got, sum int
					if err := conn.QueryRowContext(b.Context(), "SELECT COUNT(*),COALESCE(SUM(id),0) FROM go_native_bench WHERE v='bulk payload'").Scan(&got, &sum); err != nil || got != count || sum != count*(count-1)/2 {
						b.Fatalf("count=%d sum=%d err=%v", got, sum, err)
					}
				}
			})
		}
		prepared.Close()
		mariadb.DeregisterReaderHandler("gomdb_native_bench")
	}
}
