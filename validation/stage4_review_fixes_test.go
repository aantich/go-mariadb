// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

func TestMariaDBNativeCloseAfterReturn(t *testing.T) {
	for _, mode := range nativeModes {
		for _, path := range []string{"text_full", "text_early", "prepared_full", "cursor_full", "cursor_early"} {
			t.Run(mode+"/"+path, func(t *testing.T) {
				// Default pool policy is essential: managed reset would erase the
				// old IDs and hide the unsafe cleanup reported in the review.
				cfg := driverConfig(t, mode)
				if mode == "legacy" {
					cfg.DialFunc = maskEOFGreeting
				}
				if mode == "uncached" {
					cfg.DialFunc = maskSessionCacheGreeting
				}
				if strings.HasPrefix(path, "cursor") {
					if err := cfg.Apply(mariadb.WithServerCursors(16)); err != nil {
						t.Fatal(err)
					}
				}
				db := openConfig(t, cfg)
				db.SetMaxOpenConns(1)
				conn := pinned(t, db)
				var before uint64
				if err := conn.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&before); err != nil {
					t.Fatal(err)
				}
				handles := make([]*mariadb.NativeStmt, 3)
				for i := range handles {
					stmt, err := mariadb.PrepareNative(t.Context(), conn, "DO ?")
					if err != nil {
						t.Fatal(err)
					}
					handles[i] = stmt
					defer stmt.Close()
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				next := pinned(t, db)
				const query = "SELECT seq,REPEAT('x',32) FROM seq_1_to_1000 WHERE seq>=?"
				var rows *sql.Rows
				var err error
				if path == "prepared_full" {
					stmt, prepareErr := next.PrepareContext(t.Context(), query)
					if prepareErr != nil {
						t.Fatal(prepareErr)
					}
					defer stmt.Close()
					rows, err = stmt.QueryContext(t.Context(), 1)
				} else {
					rows, err = next.QueryContext(t.Context(), strings.Replace(query, "?", "1", 1))
				}
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				if !rows.Next() {
					t.Fatal("first row missing", rows.Err())
				}
				var id int
				var raw sql.RawBytes
				if err := rows.Scan(&id, &raw); err != nil || id != 1 {
					t.Fatalf("first row=%d error=%v", id, err)
				}
				want := bytes.Repeat([]byte{'x'}, 32)
				var wait sync.WaitGroup
				for _, stmt := range handles {
					wait.Add(1)
					go func() {
						defer wait.Done()
						if err := stmt.Close(); err != nil {
							t.Errorf("old close: %v", err)
						}
					}()
				}
				wait.Wait()
				if !bytes.Equal(raw, want) {
					t.Fatal("close overwrote borrowed bytes")
				}
				count := 1
				if !strings.HasSuffix(path, "early") {
					for rows.Next() {
						if err := rows.Scan(&id, &raw); err != nil || id != count+1 || !bytes.Equal(raw, want) {
							t.Fatalf("row=%d id=%d error=%v", count, id, err)
						}
						count++
					}
					if err := rows.Err(); err != nil || count != 1000 {
						t.Fatalf("read %d/1000: %v", count, err)
					}
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				var after uint64
				if err := next.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&after); err != nil || before != after {
					t.Fatalf("connection %d/%d error=%v", before, after, err)
				}
			})
		}
	}
}

func TestMariaDBNativeDeferredCloseCancellation(t *testing.T) {
	db := openConfig(t, driverConfig(t, "text"))
	db.SetMaxOpenConns(1)
	conn := pinned(t, db)
	stmt, err := mariadb.PrepareNative(t.Context(), conn, "DO ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	conn.Close()
	next := pinned(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := next.QueryContext(ctx, "SELECT seq FROM seq_1_to_1000")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var n int
	if err := rows.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	// Wait for database/sql to observe cancellation before requesting Close.
	// Otherwise Close can win its awaitDone select and legitimately leave Err
	// nil; that tests scheduler ordering rather than cancellation retirement.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !errors.Is(rows.Err(), context.Canceled) {
		select {
		case <-deadline.C:
			t.Fatal("Rows did not observe cancellation")
		case <-tick.C:
		}
	}
	rows.Close()
	if !errors.Is(rows.Err(), context.Canceled) {
		t.Fatalf("Rows error=%v", rows.Err())
	}
	next.Close()
	fresh := pinned(t, db)
	if err := fresh.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMariaDBStage4UnitValidation(t *testing.T) {
	for _, units := range [][]mariadb.BulkUnitResult{nil, {}, {{RowsAffected: 1}}, {{RowsAffected: 1}, {RowsAffected: 2}}} {
		if err := stage4CheckUnits(units, 2); err == nil {
			t.Fatalf("accepted invalid units: %v", units)
		}
	}
	if err := stage4CheckUnits([]mariadb.BulkUnitResult{{RowsAffected: 1}, {RowsAffected: 1}}, 2); err != nil {
		t.Fatal(err)
	}
}
