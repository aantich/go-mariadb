// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestNativeStatementDeferredClose(t *testing.T) {
	for _, kind := range []string{"buffered", "empty_buffer", "reset_generation", "retired", "write_failure"} {
		t.Run(kind, func(t *testing.T) {
			peer, mc, _, stmt := retainedFixture(t)
			rows := &mysqlRows{mc: mc, rs: resultSet{done: true}}
			mc.trackRows(rows)
			mc.sequence = 42
			copy(mc.buf.cachedBuf, "borrowed bytes")
			borrowed := mc.buf.cachedBuf[:14]
			want := bytes.Clone(borrowed)
			if kind == "buffered" {
				mc.buf.buf = borrowed
			}
			n := len(peer.commands)
			if err := stmt.Close(); err != nil || len(peer.commands) != n || mc.sequence != 42 || !bytes.Equal(borrowed, want) || len(mc.deferredCloses) != 1 {
				t.Fatalf("close error=%v commands=%v sequence=%d pending=%d", err, peer.commands, mc.sequence, len(mc.deferredCloses))
			}
			// The result consumer, not statement close, owns the remaining bytes.
			mc.buf.buf = nil
			switch kind {
			case "reset_generation":
				mc.generation++
			case "retired":
				mc.close()
			case "write_failure":
				peer.writeErrors = map[int]error{peer.writes + 1: io.ErrClosedPipe}
			}
			// Wire EOF may already have cleared mc; ownership must survive it.
			rows.mc = nil
			err := rows.Close()
			if kind == "write_failure" {
				if err == nil || !mc.closed.Load() {
					t.Fatalf("cleanup error=%v retired=%v", err, mc.closed.Load())
				}
			} else if kind != "retired" && err != nil {
				t.Fatal(err)
			}
			if mc.activeRows != nil || len(mc.deferredCloses) != 0 || rows.owner != nil {
				t.Fatal("Rows.Close retained cleanup ownership")
			}
			if kind == "reset_generation" || kind == "retired" {
				if len(peer.commands) != n {
					t.Fatal("stale/retired cleanup sent a command")
				}
			} else if len(peer.commands) != n+1 || peer.commands[n] != comStmtClose {
				t.Fatalf("cleanup commands=%v", peer.commands)
			}
			if err := rows.Close(); err != nil {
				t.Fatalf("repeated close: %v", err)
			}
		})
	}
}

func TestNativeStatementDeferredCloseCancellation(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	mc := &mysqlConn{buf: newBuffer(), cfg: NewConfig(), netConn: client, rawConn: client,
		closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
	mc.startWatcher()
	conn := nativeSQLConn(t, mc)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if readFaultCommand(peer) == nil {
			peer.Write(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
		}
		// Never read the eventual deferred CLOSE.
	}()
	defer func() { peer.Close(); <-done }()
	stmt, err := PrepareNative(t.Context(), conn, "DO 1")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	rows := &mysqlRows{mc: mc, rs: resultSet{done: true}}
	mc.trackRows(rows)
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := mc.watchCancel(ctx); err != nil {
		t.Fatal(err)
	}
	rows.finish = mc.finish
	err = rows.Close()
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || !mc.closed.Load() || mc.activeRows != nil || len(mc.deferredCloses) != 0 {
		t.Fatalf("cleanup error=%v closed=%v active=%v pending=%d", err, mc.closed.Load(), mc.activeRows != nil, len(mc.deferredCloses))
	}
}

func TestNativeStatementCloseOwnerTransfer(t *testing.T) {
	peer, mc, _, stmt := retainedFixture(t)
	previous := &mysqlRows{mc: mc, rs: resultSet{done: true}}
	mc.trackRows(previous)
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	previous.mc = nil // A completed decoder no longer owns the wire response.
	next := &mysqlRows{mc: mc, rs: resultSet{done: true}}
	mc.trackRows(next)
	n := len(peer.commands)
	if err := previous.Close(); err != nil || mc.activeRows != next || len(peer.commands) != n {
		t.Fatalf("previous close changed next owner: %v", err)
	}
	if err := next.Close(); err != nil || mc.activeRows != nil || len(peer.commands) != n+1 {
		t.Fatalf("next close did not release queued cleanup: %v", err)
	}
}
