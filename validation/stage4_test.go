// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

const reuseTable = "CREATE TEMPORARY TABLE go_stage4_reuse(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,v VARBINARY(200) DEFAULT 'default',b LONGBLOB,n BIGINT)"
const reuseInsert = "INSERT INTO go_stage4_reuse(id,v,b,n) VALUES (?,?,?,?)"

// Check the future benchmark's streaming generator/digest independently without
// invoking a benchmark function or requiring a server/performance measurement.
func TestMariaDBStage4Payload(t *testing.T) {
	for _, size := range []int{0, 1, 32769} {
		reader := &stage4PatternReader{remaining: size}
		got, err := io.ReadAll(reader)
		want := bytes.Repeat([]byte{0xa7}, size)
		if err != nil || !bytes.Equal(got, want) || stage4Digest(size) != fmt.Sprintf("%x", md5.Sum(want)) {
			t.Fatalf("size=%d bytes=%d error=%v", size, len(got), err)
		}
		if n, err := reader.Read(make([]byte, 1)); n != 0 || err != io.EOF {
			t.Fatalf("exhausted reader returned %d, %v", n, err)
		}
	}
}

func TestMariaDBNativeStatementReuse(t *testing.T) {
	for _, mode := range nativeModes {
		t.Run(mode, func(t *testing.T) {
			conn := pinned(t, openConfig(t, stage3Config(t, mode, mariadb.WithBulkUnitResults(true))))
			mustExec(t, conn, reuseTable)
			bulk, err := mariadb.PrepareNative(t.Context(), conn, reuseInsert)
			if err != nil {
				t.Fatal(err)
			}
			defer bulk.Close()
			for batch := range 2 {
				result, err := bulk.BulkExec(t.Context(), [][]any{
					{uint64(batch*2 + 1), mariadb.BulkDefault, []byte{0, 255}, nil},
					{uint64(batch*2 + 2), mariadb.BulkDefault, []byte{0, 255}, nil},
				})
				if err != nil || result.RowsAffectedUnsigned() != 2 {
					t.Fatalf("batch=%d result=%v error=%v", batch, result, err)
				}
			}
			var count int
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_stage4_reuse WHERE v='default' AND b=UNHEX('00FF') AND n IS NULL").Scan(&count); err != nil || count != 4 {
				t.Fatalf("bulk roundtrip count=%d error=%v", count, err)
			}
			mustExec(t, conn, "ALTER TABLE go_stage4_reuse AUTO_INCREMENT=9223372036854775808")
			var previous []mariadb.BulkUnitResult
			for i := range 3 {
				units, err := bulk.BulkExecUnits(t.Context(), [][]any{{nil, "units", nil, int64(i)}})
				if err != nil || len(units) != 1 || units[0].InsertID != uint64(1<<63)+uint64(i) || units[0].RowsAffected != 1 {
					t.Fatalf("unsigned units=%v error=%v", units, err)
				}
				if previous != nil && previous[0].InsertID != uint64(1<<63)+uint64(i-1) {
					t.Fatal("subsequent execution mutated owned units")
				}
				previous = units
			}
			// A reusable handle must resend types after NULL, signedness and
			// representation changes, rather than inheriting a previous signature.
			values := []any{int64(-3), uint64(1 << 63), nil, []byte{}, "λ"}
			want := [][]byte{[]byte("-3"), []byte(strconv.FormatUint(1<<63, 10)), nil, {}, []byte("λ")}
			for i, value := range values {
				payload := strings.Repeat(fmt.Sprint(i), 1025)
				result, err := bulk.ExecStream(t.Context(), nil, value, mariadb.StreamParam{Reader: strings.NewReader(payload), ChunkBytes: 31}, int64(i))
				if err != nil {
					t.Fatal(err)
				}
				var gotV, gotB []byte
				if err := conn.QueryRowContext(t.Context(), "SELECT v,b FROM go_stage4_reuse WHERE id=?", result.LastInsertIDUnsigned()).Scan(&gotV, &gotB); err != nil || !bytes.Equal(gotV, want[i]) || (gotV == nil) != (want[i] == nil) || string(gotB) != payload {
					t.Fatalf("type change %d: v=%q bytes=%d error=%v", i, gotV, len(gotB), err)
				}
			}
			marker := errors.New("reader interrupted")
			_, err = bulk.ExecStream(t.Context(), nil, "bad", mariadb.StreamParam{Reader: &failingStream{err: marker}, ChunkBytes: 8}, nil)
			if !errors.Is(err, marker) {
				t.Fatal(err)
			}
			result, err := bulk.ExecStream(t.Context(), nil, "reused", mariadb.StreamParam{Reader: strings.NewReader("fresh after reset")}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var payload string
			if err := conn.QueryRowContext(t.Context(), "SELECT b FROM go_stage4_reuse WHERE id=?", result.LastInsertIDUnsigned()).Scan(&payload); err != nil || payload != "fresh after reset" {
				t.Fatalf("upload reset leaked data: %q %v", payload, err)
			}
			mustExec(t, conn, "BEGIN")
			if _, err := bulk.BulkExec(t.Context(), [][]any{{uint64(20), "rollback", nil, nil}}); err != nil {
				t.Fatal(err)
			}
			mustExec(t, conn, "ROLLBACK")
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_stage4_reuse WHERE id=20").Scan(&count); err != nil || count != 0 {
				t.Fatalf("rollback count=%d error=%v", count, err)
			}
			mustExec(t, conn, "BEGIN")
			units, err := bulk.BulkExecUnits(t.Context(), [][]any{{uint64(30), "partial", nil, nil}, {uint64(1), "duplicate", nil, nil}})
			var server *mariadb.MySQLError
			if units != nil || !errors.As(err, &server) || server.Number != 1062 {
				t.Fatalf("partial units=%v error=%v", units, err)
			}
			mustExec(t, conn, "ROLLBACK")
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_stage4_reuse WHERE id=30").Scan(&count); err != nil || count != 0 {
				t.Fatalf("unit rollback count=%d error=%v", count, err)
			}
			mustExec(t, conn, "ALTER TABLE go_stage4_reuse ADD extra INT DEFAULT 7")
			if _, err := bulk.ExecStream(t.Context(), nil, "DDL", nil, nil); err != nil {
				t.Fatalf("DDL reprepare: %v", err)
			}
			if err := bulk.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := bulk.ExecStream(t.Context(), nil, "closed", nil, nil); !errors.Is(err, mariadb.ErrNativeStmtClosed) {
				t.Fatal(err)
			}
			if err := conn.PingContext(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMariaDBNativeStatementReset(t *testing.T) {
	for _, mode := range nativeModes {
		t.Run(mode, func(t *testing.T) {
			db := openConfig(t, stage3Config(t, mode, mariadb.WithBulkUnitResults(true)))
			db.SetMaxOpenConns(1)
			conn := pinned(t, db)
			mustExec(t, conn, reuseTable)
			stmt, err := mariadb.PrepareNative(t.Context(), conn, reuseInsert)
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			for generation := range 3 {
				if generation > 0 {
					if err := mariadb.ResetSession(t.Context(), conn); err != nil {
						t.Fatal(err)
					}
					mustExec(t, conn, reuseTable)
				}
				if _, err := stmt.BulkExec(t.Context(), [][]any{{uint64(1), "bulk", nil, nil}}); err != nil {
					t.Fatal(err)
				}
				if _, err := stmt.BulkExecUnits(t.Context(), [][]any{{uint64(2), "unit", nil, nil}}); err != nil {
					t.Fatal(err)
				}
				if _, err := stmt.ExecStream(t.Context(), uint64(3), "stream", mariadb.StreamParam{Reader: strings.NewReader("body")}, nil); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_stage4_reuse").Scan(&count); err != nil || count != 3 {
					t.Fatalf("generation=%d count=%d error=%v", generation, count, err)
				}
			}
			// Return the connection, let managed checkout reset/reuse it, then
			// close the old native handle. Its old ID must not close a new handle.
			conn.Close()
			next := pinned(t, db)
			mustExec(t, next, reuseTable)
			fresh, err := mariadb.PrepareNative(t.Context(), next, reuseInsert)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if err := stmt.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := fresh.ExecStream(t.Context(), uint64(1), "new owner", nil, nil); err != nil {
				t.Fatalf("old cleanup damaged reused connection: %v", err)
			}
		})
	}
}

func TestMariaDBNativeStatementConcurrent(t *testing.T) {
	conn := pinned(t, openConfig(t, stage3Config(t, "text")))
	mustExec(t, conn, reuseTable)
	stmt, err := mariadb.PrepareNative(t.Context(), conn, reuseInsert)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := stmt.ExecStream(t.Context(), uint64(i+1), "parallel", mariadb.StreamParam{Reader: strings.NewReader(fmt.Sprint(i))}, int64(i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count, sum int
	if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*),SUM(n) FROM go_stage4_reuse").Scan(&count, &sum); err != nil || count != 12 || sum != 66 {
		t.Fatalf("count=%d sum=%d error=%v", count, sum, err)
	}
}

func TestMariaDBNativeStatementCancellation(t *testing.T) {
	conn := pinned(t, openConfig(t, stage3Config(t, "text")))
	stmt, err := mariadb.PrepareNative(t.Context(), conn, "SELECT SLEEP(?)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := stmt.ExecStream(ctx, 2); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("unsafe cancellation error: %v", err)
	}
	if err := conn.PingContext(t.Context()); err == nil {
		t.Fatal("canceled native connection remained usable")
	}
}
