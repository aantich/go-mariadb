// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestDedicatedTerminatorValidation(t *testing.T) {
	for _, deprecated := range []bool{false, true} {
		for _, payload := range [][]byte{{iEOF}, {iEOF, 0}, {iEOF, 0xfc}, {iEOF, 0, 0}, {iEOF, 0, 0, 0}} {
			for _, path := range []string{"drain", "text", "binary", "metadata"} {
				if deprecated && path == "metadata" {
					continue
				} // no metadata EOF in this mode
				t.Run(fmt.Sprintf("deprecated=%v/%s/%x", deprecated, path, payload), func(t *testing.T) {
					conn, mc := newRWMockConn(0)
					if deprecated {
						mc.capabilities = clientDeprecateEOF
					}
					conn.data = makePacket(0, payload)
					rows := mysqlRows{mc: mc, rs: resultSet{columns: []mysqlField{{fieldType: fieldTypeString}}}}
					var err error
					switch path {
					case "drain":
						err = mc.skipRows()
					case "text":
						err = (&textRows{rows}).Next(make([]driver.Value, 1))
					case "binary":
						err = (&binaryRows{rows}).Next(make([]driver.Value, 1))
					case "metadata":
						err = mc.skipEof()
					}
					if !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
						t.Fatalf("error=%v retired=%v", err, mc.closed.Load())
					}
				})
			}
		}
	}
	for _, payload := range [][]byte{{iEOF, 0xfb, 0, 2, 0, 0, 0}, {iEOF, 0, 0xff, 2, 0, 0, 0}, {iEOF, 0, 0, 2, 0}} {
		_, mc := newRWMockConn(0)
		mc.capabilities = clientDeprecateEOF
		if _, err := mc.terminatorStatus(payload); !errors.Is(err, ErrMalformPkt) {
			t.Fatalf("invalid OK terminator accepted: %x", payload)
		}
	}
}

func TestDedicatedTerminatorStatusAndServerErrors(t *testing.T) {
	for _, deprecated := range []bool{false, true} {
		conn, mc := newRWMockConn(0)
		payload := []byte{iEOF, 0, 0, byte(statusMoreResultsExists), 0}
		if deprecated {
			mc.capabilities = clientDeprecateEOF
			payload = append(payload, 0, 0)
		}
		conn.data = makePacket(0, payload)
		if err := mc.skipRows(); err != nil || mc.status != statusMoreResultsExists || mc.closed.Load() {
			t.Fatalf("status=%v retired=%v error=%v", mc.status, mc.closed.Load(), err)
		}
	}
	conn, mc := newRWMockConn(0)
	conn.data = makePacket(0, []byte{iERR, 0x1e, 0x04, '#', '4', '2', 'S', '0', '2'})
	err := mc.skipEof()
	var server *MySQLError
	if !errors.As(err, &server) || server.Number != 1054 || mc.closed.Load() {
		t.Fatalf("legitimate server error lost: %v", err)
	}
}

func TestDedicatedExecCachedMetadataEOF(t *testing.T) {
	for _, deprecated := range []bool{false, true} {
		for _, count := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("deprecated=%v/rows=%d", deprecated, count), func(t *testing.T) {
				conn, mc := newRWMockConn(0)
				mc.extCapabilities = clientCacheMetadata
				stmt := &mysqlStmt{mc: mc, id: 1, columns: []mysqlField{{fieldType: fieldTypeLong}}}
				response := makePacket(1, []byte{1, 0})
				sequence := byte(2)
				end := []byte{iEOF, 0, 0, 2, 0}
				if deprecated {
					mc.capabilities = clientDeprecateEOF
					end = append(end, 0, 0)
				} else {
					response = append(response, makePacket(sequence, end)...)
					sequence++
				}
				for range count {
					response = append(response, makePacket(sequence, []byte{0, 0, 123, 0, 0, 0})...)
					sequence++
				}
				response = append(response, makePacket(sequence, end)...)
				conn.queuedReplies = [][]byte{response, makePacket(1, []byte{iOK, 0, 0, 2, 0, 0, 0})}
				if _, err := stmt.Exec(nil); err != nil {
					t.Fatal(err)
				}
				if mc.buf.busy() {
					t.Fatalf("%d unread bytes", mc.buf.len())
				}
				if _, err := mc.Exec("DO 1", nil); err != nil {
					t.Fatal("next command:", err)
				}
			})
		}
	}
}

func TestDedicatedMalformedRowsRetire(t *testing.T) {
	for _, binary := range []bool{false, true} {
		for _, payload := range [][]byte{{0xfc}, {1, 'x', 0}} {
			if binary {
				payload = append([]byte{0, 0}, payload...)
			}
			rows, _, mc := rowsTestPacket(binary, []mysqlField{{fieldType: fieldTypeString}}, payload, false)
			if err := rows.Next(make([]driver.Value, 1)); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
				t.Fatalf("binary=%v err=%v retired=%v", binary, err, mc.closed.Load())
			}
		}
	}
}

func TestDedicatedCloseKeepsCancellation(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	mc := &mysqlConn{buf: newBuffer(), cfg: NewConfig(), netConn: client, rawConn: client, closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket}
	defer mc.close()
	mc.startWatcher()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := mc.watchCancel(ctx); err != nil {
		t.Fatal(err)
	}
	rows := &textRows{mysqlRows{mc: mc, finish: mc.finish, rs: resultSet{columns: []mysqlField{{fieldType: fieldTypeString}}}}}
	// A healthy first row does not authorize abandoning the query deadline
	// while Close waits for the peer to finish the remaining response.
	go func() { peer.Write(makePacket(0, []byte{1, 'x'})) }()
	if err := rows.Next(make([]driver.Value, 1)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- rows.Close() }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || !mc.closed.Load() {
			t.Fatalf("close=%v retired=%v", err, mc.closed.Load())
		}
	case <-time.After(3 * time.Second):
		peer.Close()
		<-done
		t.Fatal("Close abandoned query cancellation while draining")
	}
	if err := rows.Close(); err != nil {
		t.Fatal("repeated Close:", err)
	}
}

func TestDedicatedBulkZeroTimeLocations(t *testing.T) {
	for _, location := range []*time.Location{time.UTC, time.FixedZone("west", -8*3600), time.FixedZone("east", 9*3600)} {
		_, mc := newRWMockConn(0)
		mc.cfg.Loc = location
		packet, err := (&mysqlStmt{mc: mc, id: 1, paramCount: 1}).encodeBulk([][]any{{time.Time{}}})
		if err != nil {
			t.Fatal(err)
		}
		value, _, _, err := readLengthEncodedString(packet[14:])
		if err != nil || string(value) != "0000-00-00" {
			t.Fatalf("location=%v value=%q err=%v", location, value, err)
		}
	}
}

func FuzzDedicatedTerminator(f *testing.F) {
	for _, seed := range [][]byte{{iEOF}, {iEOF, 0, 0, 2, 0}, {iEOF, 0, 0, 2, 0, 0, 0}, {iEOF, 0xfc}, {iEOF, 0xfb, 0, 0, 0}} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, payload []byte, deprecated bool) {
		_, mc := newRWMockConn(0)
		if deprecated {
			mc.capabilities = clientDeprecateEOF
		}
		_, err := mc.terminatorStatus(payload)
		if err != nil && (!errors.Is(err, ErrMalformPkt) || !mc.closed.Load()) {
			t.Fatalf("invalid error=%v", err)
		}
	})
}
