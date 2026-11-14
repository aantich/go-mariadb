// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

func TestMariaDBReviewBulkTransports(t *testing.T) {
	for _, mode := range []string{"text", "compressed", "tls", "socket", "x509"} {
		t.Run(mode, func(t *testing.T) {
			cfg := driverConfig(t, mode)
			if err := cfg.Apply(mariadb.WithBulkOperations(true)); err != nil {
				t.Fatal(err)
			}
			db := openConfig(t, cfg)
			db.SetMaxOpenConns(1)
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_review_bulk(id INT PRIMARY KEY,s VARCHAR(20) DEFAULT 'fallback')")
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_review_blob(v LONGBLOB)")
			payload := bytes.Repeat([]byte("abcdef\x00z"), 1024)
			if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_blob VALUES (?)", [][]any{{payload}}); err != nil {
				t.Fatal(err)
			}
			var blob []byte
			if err := conn.QueryRowContext(t.Context(), "SELECT v FROM go_review_blob").Scan(&blob); err != nil || !bytes.Equal(blob, payload) {
				t.Fatalf("bulk BLOB mismatch: bytes=%d err=%v", len(blob), err)
			}
			result, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_bulk VALUES (?,?)", [][]any{{int64(1), "preserve"}, {int64(2), nil}, {int64(3), mariadb.BulkDefault}})
			if err != nil || result.RowsAffectedUnsigned() != 3 {
				t.Fatalf("result=%v err=%v", result, err)
			}
			if _, err := mariadb.BulkExec(t.Context(), conn, "UPDATE go_review_bulk SET s=? WHERE id=?", [][]any{{mariadb.BulkIgnore, int64(1)}}); err != nil {
				t.Fatal(err)
			}
			var value string
			var null bool
			if err := conn.QueryRowContext(t.Context(), "SELECT s FROM go_review_bulk WHERE id=1").Scan(&value); err != nil || value != "preserve" {
				t.Fatalf("IGNORE=%q err=%v", value, err)
			}
			if err := conn.QueryRowContext(t.Context(), "SELECT s IS NULL FROM go_review_bulk WHERE id=2").Scan(&null); err != nil || !null {
				t.Fatalf("NULL=%v err=%v", null, err)
			}
			if err := conn.QueryRowContext(t.Context(), "SELECT s FROM go_review_bulk WHERE id=3").Scan(&value); err != nil || value != "fallback" {
				t.Fatalf("DEFAULT=%q err=%v", value, err)
			}
			if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_bulk(id) VALUES (?)", [][]any{{int64(1)}}); err == nil {
				t.Fatal("duplicate accepted")
			}
			// A rejected write must not leave a pending reply for the next command.
			if err := conn.QueryRowContext(t.Context(), "SELECT 'still synchronized'").Scan(&value); err != nil || value != "still synchronized" {
				t.Fatalf("value=%q err=%v", value, err)
			}
			var before, after uint64
			if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&before); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			_, err = mariadb.BulkExec(ctx, conn, "UPDATE go_review_bulk SET s=SLEEP(2) WHERE id=?", [][]any{{int64(1)}})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancellation=%v", err)
			}
			conn.Close()
			if err := db.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&after); err != nil || before == after {
				t.Fatalf("canceled session reused: before=%d after=%d err=%v", before, after, err)
			}
		})
	}
}

func TestMariaDBReviewZeroTimeParity(t *testing.T) {
	for _, offset := range []int{0, -8 * 3600, 9 * 3600} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			cfg := driverConfig(t, "text")
			cfg.Loc = time.FixedZone("fixture", offset)
			cfg.Apply(mariadb.WithBulkOperations(true), mariadb.TimeTruncate(10*time.Microsecond))
			conn, err := openConfig(t, cfg).Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mustExec(t, conn, "SET SESSION sql_mode=''")
			defer conn.ExecContext(context.Background(), "SET SESSION sql_mode=DEFAULT")
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_review_dates(id INT PRIMARY KEY,d DATETIME(6))")
			values := []time.Time{{}, time.Date(2026, 11, 12, 12, 13, 14, 123456789, time.UTC)}
			for i, value := range values {
				mustExec(t, conn, "INSERT INTO go_review_dates VALUES (?,?)", i*2, value)
				if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_dates VALUES (?,?)", [][]any{{int64(i*2 + 1), value}}); err != nil {
					t.Fatal(err)
				}
				var ordinary, bulk string
				if err := conn.QueryRowContext(t.Context(), "SELECT CAST(a.d AS CHAR),CAST(b.d AS CHAR) FROM go_review_dates a JOIN go_review_dates b ON b.id=a.id+1 WHERE a.id=?", i*2).Scan(&ordinary, &bulk); err != nil || ordinary != bulk {
					t.Fatalf("ordinary=%q bulk=%q err=%v", ordinary, bulk, err)
				}
			}
			if _, err := mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_dates VALUES (?,?)", [][]any{{int64(99), time.Date(10000, 1, 2, 0, 0, 0, 0, time.UTC)}}); err == nil {
				t.Fatal("invalid year accepted")
			}
			var count int
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_review_dates WHERE id=99").Scan(&count); err != nil || count != 0 {
				t.Fatalf("validation partially executed: count=%d err=%v", count, err)
			}
		})
	}
}

// maskEOFGreeting exercises a supported protocol negotiation without modifying
// either private driver state or the shared server's configuration.
func maskEOFGreeting(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			conn.Close()
		}
	}()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n > 4096 {
		return nil, fmt.Errorf("oversized fixture greeting")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	if len(body) < 2 {
		return nil, fmt.Errorf("truncated fixture greeting")
	}
	end := bytes.IndexByte(body[1:], 0) + 1
	if end < 1 || end+20 >= len(body) {
		return nil, fmt.Errorf("malformed fixture greeting")
	}
	body[end+20] &^= 1 // CLIENT_DEPRECATE_EOF = 1 << 24
	conn.SetReadDeadline(time.Time{})
	failed = false
	return &greetingControl{Conn: conn, reader: io.MultiReader(bytes.NewReader(append(header[:], body...)), conn), received: &atomic.Uint64{}}, nil
}

func TestMariaDBReviewCachedMetadataEOF(t *testing.T) {
	cfg := driverConfig(t, "text")
	cfg.DialFunc = maskEOFGreeting
	conn, err := openConfig(t, cfg).Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, query := range []string{"SELECT 123", "SELECT 123 WHERE 0", "SELECT 123 UNION ALL SELECT 456"} {
		stmt, err := conn.PrepareContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if _, err := stmt.ExecContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := stmt.QueryContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var n int
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
		stmt.Close()
	}
	var next int
	if err := conn.QueryRowContext(t.Context(), "SELECT 789").Scan(&next); err != nil || next != 789 {
		t.Fatalf("next=%d err=%v", next, err)
	}
}

func TestMariaDBReviewScanErrorReuse(t *testing.T) {
	conn, err := openDB(t, "text").Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var before, after uint64
	if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	var incompatible int
	if err := conn.QueryRowContext(t.Context(), "SELECT 'not an integer'").Scan(&incompatible); err == nil {
		t.Fatal("incompatible destination accepted")
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&after); err != nil || before != after {
		t.Fatalf("healthy session discarded: before=%d after=%d err=%v", before, after, err)
	}
}

func TestMariaDBReviewBulkRollback(t *testing.T) {
	for _, mode := range []string{"text", "compressed"} {
		t.Run(mode, func(t *testing.T) {
			cfg := driverConfig(t, mode)
			if err := cfg.Apply(mariadb.WithBulkOperations(true)); err != nil {
				t.Fatal(err)
			}
			conn, err := openConfig(t, cfg).Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_review_rollback(id INT PRIMARY KEY) ENGINE=InnoDB")
			mustExec(t, conn, "BEGIN")
			defer conn.ExecContext(context.Background(), "ROLLBACK")
			_, err = mariadb.BulkExec(t.Context(), conn, "INSERT INTO go_review_rollback VALUES (?)", [][]any{{int64(1)}, {int64(1)}})
			var server *mariadb.MySQLError
			if !errors.As(err, &server) || server.Number != 1062 {
				t.Fatalf("server error=%v", err)
			}
			mustExec(t, conn, "ROLLBACK")
			var count int
			if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM go_review_rollback").Scan(&count); err != nil || count != 0 {
				t.Fatalf("rollback count=%d err=%v", count, err)
			}
		})
	}
}
