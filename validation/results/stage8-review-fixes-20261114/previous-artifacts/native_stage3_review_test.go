// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
)

func TestNativeCursorMaterializationError(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, malformed := range []bool{false, true} {
			t.Run(fmt.Sprintf("legacy=%t/malformed=%t", legacy, malformed), func(t *testing.T) {
				peer, mc := newRWMockConn(0)
				mc.cfg.cursorFetchRows = 2
				if !legacy {
					mc.capabilities = clientDeprecateEOF
				}
				response := append(makePacket(1, []byte{1}), makePacket(2, nativeColumn("v", fieldTypeLong))...)
				terminal := []byte{iERR, 0xda, 0x04, '#', '2', '1', '0', '0', '0', 's', 'u', 'b', 'q', 'u', 'e', 'r', 'y'}
				if malformed {
					terminal = terminal[:7] // Incomplete SQLSTATE is a protocol failure.
				}
				response = append(response, makePacket(3, terminal)...)
				peer.queuedReplies = [][]byte{response, makePacket(1, []byte{iOK, 0, 0, 2, 0, 0, 0})}
				stmt := &mysqlStmt{mc: mc, id: 1}
				rows, err := stmt.queryCursor(nil)
				if rows != nil {
					t.Fatal("failed materialization returned rows")
				}
				if malformed {
					if !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
						t.Fatalf("malformed error=%v closed=%v", err, mc.closed.Load())
					}
					return
				}
				var server *MySQLError
				if !errors.As(err, &server) || server.Number != 1242 || string(server.SQLState[:]) != "21000" || mc.closed.Load() {
					t.Fatalf("SQL identity/reuse: error=%v closed=%v", err, mc.closed.Load())
				}
				if err := mc.Ping(t.Context()); err != nil {
					t.Fatalf("terminal SQL error left unread data: %v", err)
				}
			})
		}
	}
}

func TestNativeProgressCallbackBadConn(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, sqlFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("wrapped=%t/sql_failure=%t", wrapped, sqlFailure), func(t *testing.T) {
				peer, mc := newRWMockConn(0)
				mc.extCapabilities = progressIndicator
				terminal := []byte{iOK, 1, 0, 2, 0, 0, 0}
				if sqlFailure {
					terminal = []byte{iERR, 0x26, 0x04, '#', '2', '3', '0', '0', '0', 'd', 'u', 'p'}
				}
				response := append(makePacket(1, progressPacket("copy")), makePacket(2, terminal)...)
				peer.queuedReplies = [][]byte{response, makePacket(1, []byte{iOK, 0, 0, 2, 0, 0, 0})}
				conn := nativeSQLConn(t, mc)
				marker := driver.ErrBadConn
				if wrapped {
					marker = fmt.Errorf("application callback: %w", marker)
				}
				result, err := ExecWithProgress(t.Context(), conn, func(Progress) error { return marker }, "ALTER TABLE t ADD v INT")
				if !errors.Is(err, marker) || mc.closed.Load() || mc.progress != nil {
					t.Fatalf("callback identity/reuse: error=%v closed=%v", err, mc.closed.Load())
				}
				var server *MySQLError
				if sqlFailure {
					if result != nil || !errors.As(err, &server) || server.Number != 1062 || string(server.SQLState[:]) != "23000" {
						t.Fatalf("lost SQL error: result=%v error=%v", result, err)
					}
				} else if result == nil || result.RowsAffectedUnsigned() != 1 || errors.As(err, &server) {
					t.Fatalf("lost successful result: result=%v error=%v", result, err)
				}
				if err := conn.PingContext(t.Context()); err != nil {
					t.Fatalf("callback retired a healthy pinned connection: %v", err)
				}
			})
		}
	}
}

// Reply to commands, not a predicted chunk count. A boundary regression must
// reach the packet-size assertion rather than waiting for a missing mock ACK.
type streamPacketPeer struct{ *mockConn }

func (peer *streamPacketPeer) Write(data []byte) (int, error) {
	peer.queuedReplies = nil
	switch data[4] {
	case comStmtPrepare:
		prepare := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, []byte{0})...)
		peer.queuedReplies = [][]byte{prepare}
	case comStmtExecute:
		peer.queuedReplies = [][]byte{makePacket(1, []byte{iOK, 1, 0, 2, 0, 0, 0})}
	}
	return peer.mockConn.Write(data)
}

func TestNativeStreamPacketBoundary(t *testing.T) {
	const limit = 65536
	for _, size := range []int{limit - 8, limit - 7, limit - 6} {
		for _, chunk := range []int{0, 65536, 1 << 20} {
			t.Run(fmt.Sprintf("bytes=%d/chunk=%d", size, chunk), func(t *testing.T) {
				peer, mc := newRWMockConn(0)
				peer.maxReads = 100
				mc.netConn = &streamPacketPeer{peer}
				mc.maxAllowedPacket = limit
				mc.capabilities = clientDeprecateEOF
				payload := bytes.Repeat([]byte{'x'}, size)
				result, err := ExecStream(t.Context(), nativeSQLConn(t, mc), "INSERT INTO t VALUES (?)", StreamParam{Reader: bytes.NewReader(payload), ChunkBytes: chunk})
				if err != nil || result == nil || result.RowsAffectedUnsigned() != 1 {
					t.Fatalf("result=%v error=%v", result, err)
				}
				var actual []byte
				for data := peer.written; len(data) > 0; {
					n := getUint24(data[:3])
					if data[4] == comStmtSendLongData {
						if n >= limit {
							t.Fatalf("LONG_DATA body=%d reaches exclusive server limit=%d", n, limit)
						}
						actual = append(actual, data[11:4+n]...)
					}
					data = data[4+n:]
				}
				if !bytes.Equal(actual, payload) {
					t.Fatal("chunk boundary lost parameter bytes")
				}
			})
		}
	}
}
