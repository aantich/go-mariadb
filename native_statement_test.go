// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// Respond by command so tests can assert the number of wire commands without
// predicting the implementation's chunking or cleanup schedule.
type nativeStatementPeer struct {
	*mockConn
	commands []byte
	ids      []uint32
	nextID   uint32
	params   int
	failNext bool
}

func (p *nativeStatementPeer) Write(data []byte) (int, error) {
	command := data[4]
	p.commands = append(p.commands, command)
	p.queuedReplies = nil
	if command == comStmtPrepare {
		p.nextID++
		body := binary.LittleEndian.AppendUint32([]byte{iOK}, p.nextID)
		body = append(body, 0, 0, byte(p.params), byte(p.params>>8), 0, 0, 0)
		response := makePacket(1, body)
		for i := 0; i < p.params; i++ {
			response = append(response, makePacket(byte(i+2), nativeColumn("?", fieldTypeString))...)
		}
		p.queuedReplies = [][]byte{response}
	} else if command != comStmtClose && command != comStmtSendLongData {
		response := makePacket(1, []byte{iOK, 1, 0, 2, 0, 0, 0})
		if p.failNext {
			p.failNext = false
			response = makePacket(1, []byte{iERR, 0x26, 0x04, '#', '2', '3', '0', '0', '0', 'd', 'u', 'p'})
		} else if command == 0xfa && data[9]&64 != 0 {
			response = makePacket(1, []byte{2})
			response = append(response, makePacket(2, nativeColumn("Id", fieldTypeLongLong))...)
			response = append(response, makePacket(3, nativeColumn("Affected_rows", fieldTypeLongLong))...)
			row := binary.LittleEndian.AppendUint64([]byte{0, 0}, 1<<63)
			row = binary.LittleEndian.AppendUint64(row, 1)
			response = append(response, makePacket(4, row)...)
			response = append(response, makePacket(5, []byte{iEOF, 0, 0, 2, 0, 0, 0})...)
		}
		p.queuedReplies = [][]byte{response}
	}
	if command == comStmtExecute || command == 0xfa || command == comStmtClose {
		p.ids = append(p.ids, binary.LittleEndian.Uint32(data[5:9]))
	}
	return p.mockConn.Write(data)
}

func retainedFixture(t *testing.T) (*nativeStatementPeer, *mysqlConn, *sql.Conn, *NativeStmt) {
	t.Helper()
	base, mc := newRWMockConn(0)
	base.maxReads = 100
	peer := &nativeStatementPeer{mockConn: base, params: 1}
	mc.netConn = peer
	mc.capabilities = clientDeprecateEOF
	mc.extCapabilities = clientStmtBulkOperations | clientUnitBulkResult
	conn := nativeSQLConn(t, mc)
	stmt, err := PrepareNative(t.Context(), conn, "INSERT INTO t VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stmt.Close() })
	return peer, mc, conn, stmt
}

func TestNativeStatementReuse(t *testing.T) {
	for _, kind := range []string{"aggregate", "units", "stream"} {
		t.Run(kind, func(t *testing.T) {
			peer, mc, _, stmt := retainedFixture(t)
			for range 3 {
				switch kind {
				case "aggregate":
					result, err := stmt.BulkExec(t.Context(), [][]any{{1}})
					if err != nil || result.RowsAffectedUnsigned() != 1 {
						t.Fatalf("result=%v error=%v", result, err)
					}
				case "units":
					units, err := stmt.BulkExecUnits(t.Context(), [][]any{{1}})
					if err != nil || len(units) != 1 || units[0].InsertID != 1<<63 || units[0].RowsAffected != 1 {
						t.Fatalf("units=%v error=%v", units, err)
					}
				case "stream":
					_, err := stmt.ExecStream(t.Context(), StreamParam{Reader: strings.NewReader("fresh"), ChunkBytes: 2})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := stmt.Close(); err != nil {
				t.Fatal(err)
			}
			counts := map[byte]int{}
			for _, command := range peer.commands {
				counts[command]++
			}
			if counts[comStmtPrepare] != 1 || counts[comStmtClose] != 1 || len(mc.nativeStatements) != 0 {
				t.Fatalf("prepare/close counts=%v registry=%d", counts, len(mc.nativeStatements))
			}
			for _, id := range peer.ids {
				if id != 1 {
					t.Fatalf("changed statement ID without reset: %v", peer.ids)
				}
			}
		})
	}
}

func TestNativeStatementLifetime(t *testing.T) {
	t.Run("connection_return_before_close", func(t *testing.T) {
		peer, mc, conn, stmt := retainedFixture(t)
		conn.Close()
		if _, err := stmt.ExecStream(t.Context(), "x"); !errors.Is(err, sql.ErrConnDone) {
			t.Fatal(err)
		}
		if err := stmt.Close(); err != nil || len(mc.nativeStatements) != 0 || peer.commands[len(peer.commands)-1] != comStmtClose {
			t.Fatalf("cleanup error=%v registry=%d commands=%v", err, len(mc.nativeStatements), peer.commands)
		}
	})
	t.Run("stale_close_does_not_close_reused_id", func(t *testing.T) {
		peer, mc, conn, first := retainedFixture(t)
		conn.Raw(func(any) error { mc.generation++; return nil })
		peer.nextID = 0 // The server may recycle IDs after a session reset.
		second, err := PrepareNative(t.Context(), conn, "INSERT INTO t VALUES (?)")
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		n := len(peer.commands)
		if err := first.Close(); err != nil || len(peer.commands) != n || len(mc.nativeStatements) != 1 {
			t.Fatalf("stale close error=%v commands=%v registry=%d", err, peer.commands, len(mc.nativeStatements))
		}
		if _, err := second.ExecStream(t.Context(), "safe"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("closed_zero_and_empty", func(t *testing.T) {
		peer, _, _, stmt := retainedFixture(t)
		n := len(peer.commands)
		if _, err := stmt.BulkExec(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := stmt.BulkExecUnits(t.Context(), nil); err != nil || len(peer.commands) != n {
			t.Fatalf("empty input sent SQL: %v", err)
		}
		stmt.Close()
		if _, err := stmt.BulkExec(t.Context(), nil); !errors.Is(err, ErrNativeStmtClosed) {
			t.Fatal(err)
		}
		var zero NativeStmt
		if _, err := zero.ExecStream(t.Context()); !errors.Is(err, ErrNativeStmtClosed) || zero.Close() != nil {
			t.Fatal(err)
		}
	})
}

func TestNativeStatementResetAndValidation(t *testing.T) {
	for _, changedArity := range []bool{false, true} {
		t.Run(fmt.Sprint(changedArity), func(t *testing.T) {
			peer, mc, conn, stmt := retainedFixture(t)
			conn.Raw(func(any) error { mc.generation++; return nil })
			if changedArity {
				peer.params = 2
			}
			_, err := stmt.ExecStream(t.Context(), "value")
			if changedArity {
				if err == nil || mc.closed.Load() || peer.commands[len(peer.commands)-1] != comStmtClose {
					t.Fatalf("arity error=%v closed=%v commands=%v", err, mc.closed.Load(), peer.commands)
				}
			} else if err != nil || peer.ids[len(peer.ids)-1] != 2 {
				t.Fatalf("reprepare error=%v ids=%v", err, peer.ids)
			}
		})
	}
	t.Run("validation_and_server_error_reuse", func(t *testing.T) {
		peer, mc, _, stmt := retainedFixture(t)
		n := len(peer.commands)
		if _, err := stmt.ExecStream(t.Context(), struct{}{}); err == nil || len(peer.commands) != n {
			t.Fatalf("invalid argument sent SQL: %v", err)
		}
		peer.failNext = true
		_, err := stmt.ExecStream(t.Context(), "duplicate")
		var server *MySQLError
		if !errors.As(err, &server) || server.Number != 1062 || mc.closed.Load() {
			t.Fatal(err)
		}
		if _, err := stmt.ExecStream(t.Context(), uint64(1)<<63); err != nil {
			t.Fatal(err)
		}
		mc.extCapabilities = 0
		n = len(peer.commands)
		if _, err := stmt.BulkExec(t.Context(), [][]any{{1}}); !errors.Is(err, ErrUnsupportedCapability) || len(peer.commands) != n {
			t.Fatal(err)
		}
	})
}

func TestNativeStatementCanceledWait(t *testing.T) {
	_, mc, _, stmt := retainedFixture(t)
	stmt.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := stmt.ExecStream(ctx, "unused"); !errors.Is(err, context.DeadlineExceeded) || mc.closed.Load() {
		t.Fatalf("wait cancellation error=%v closed=%v", err, mc.closed.Load())
	}
	<-stmt.gate
	if _, err := stmt.ExecStream(t.Context(), "healthy"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStatementNoReplay(t *testing.T) {
	for _, kind := range []string{"aggregate", "units", "stream", "valuer_panic", "reader_panic"} {
		t.Run(kind, func(t *testing.T) {
			peer, mc, _, stmt := retainedFixture(t)
			if strings.HasSuffix(kind, "panic") {
				defer func() {
					if recover() == nil || !mc.closed.Load() {
						t.Fatal("panic did not retire the connection")
					}
				}()
				if kind == "valuer_panic" {
					stmt.BulkExec(t.Context(), [][]any{{panicNativeValuer{}}})
				} else {
					stmt.ExecStream(t.Context(), StreamParam{Reader: panicReader{}})
				}
				t.Fatal("missing panic")
			}
			peer.writeErrors = map[int]error{peer.writes + 1: io.ErrClosedPipe}
			var err error
			switch kind {
			case "aggregate":
				_, err = stmt.BulkExec(t.Context(), [][]any{{1}})
			case "units":
				_, err = stmt.BulkExecUnits(t.Context(), [][]any{{1}})
			case "stream":
				_, err = stmt.ExecStream(t.Context(), "once")
			}
			if err == nil || errors.Is(err, driver.ErrBadConn) || !mc.closed.Load() {
				t.Fatalf("error=%v closed=%v", err, mc.closed.Load())
			}
		})
	}
}

func TestNativeStatementCancellation(t *testing.T) {
	for _, phase := range []string{"prepare_reply", "execute_write", "execute_reply", "reprepare_reply"} {
		t.Run(phase, func(t *testing.T) {
			client, peer := net.Pipe()
			cfg := NewConfig()
			mc := &mysqlConn{buf: newBuffer(), cfg: cfg, netConn: client, rawConn: client,
				closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
			mc.startWatcher()
			conn := nativeSQLConn(t, mc)
			done := make(chan struct{})
			go func() {
				defer close(done)
				if readFaultCommand(peer) != nil || phase == "prepare_reply" {
					return
				}
				prepare := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, nativeColumn("?", fieldTypeString))...)
				if _, err := peer.Write(prepare); err != nil || phase == "execute_write" {
					return
				}
				readFaultCommand(peer) // Consume EXECUTE or the reset-generation PREPARE; never reply.
			}()
			defer func() { peer.Close(); <-done }()
			prepareCtx := t.Context()
			if phase == "prepare_reply" {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
				defer cancel()
				prepareCtx = ctx
			}
			stmt, err := PrepareNative(prepareCtx, conn, "INSERT INTO t VALUES (?)")
			if phase != "prepare_reply" {
				if err != nil {
					t.Fatal(err)
				}
				defer stmt.Close()
				if phase == "reprepare_reply" {
					conn.Raw(func(any) error { mc.generation++; return nil })
				}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
				defer cancel()
				_, err = stmt.ExecStream(ctx, "once")
			}
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || !mc.closed.Load() {
				t.Fatalf("phase=%s error=%v closed=%v", phase, err, mc.closed.Load())
			}
		})
	}
}

func TestNativeStatementOrdinaryPreparation(t *testing.T) {
	base, mc := newRWMockConn(0)
	peer := &nativeStatementPeer{mockConn: base, params: 1}
	mc.netConn, mc.capabilities = peer, clientDeprecateEOF
	conn := nativeSQLConn(t, mc)
	stmt, err := conn.PrepareContext(t.Context(), "SELECT ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if mc.nativeStatements != nil {
		t.Fatal("ordinary prepare allocated a native registry")
	}
}

func TestNativeStatementCloseTimeout(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	cfg := NewConfig()
	cfg.WriteTimeout = 30 * time.Millisecond
	mc := &mysqlConn{buf: newBuffer(), cfg: cfg, netConn: client, rawConn: client,
		closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
	mc.startWatcher()
	conn := nativeSQLConn(t, mc)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if readFaultCommand(peer) == nil {
			// Return a successful zero-parameter prepare, then never read CLOSE.
			peer.Write(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
		}
	}()
	defer func() { peer.Close(); <-done }()
	stmt, err := PrepareNative(t.Context(), conn, "DO 1")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	err = stmt.Close()
	// Statement cleanup may expose database/sql's bad-connection error rather
	// than the transport timeout. Require bounded failure, retirement and unlink.
	if err == nil || !mc.closed.Load() || len(mc.nativeStatements) != 0 {
		t.Fatalf("close error=%v closed=%v registry=%d", err, mc.closed.Load(), len(mc.nativeStatements))
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
