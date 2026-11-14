// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
)

func progressPacket(info string) []byte {
	return appendLengthEncodedString([]byte{0xff, 0xff, 0xff, 1, 1, 3, 0x50, 0xc3, 0}, info)
}

func nativeSQLConn(t *testing.T, mc *mysqlConn) *sql.Conn {
	t.Helper()
	db := sql.OpenDB(bulkFaultConnector{mc})
	t.Cleanup(func() { db.Close() })
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestNativeProgressPackets(t *testing.T) {
	t.Run("owned_ordered_iterative", func(t *testing.T) {
		peer, mc := newRWMockConn(0)
		mc.extCapabilities = progressIndicator
		var reports []Progress
		mc.progress = &progressState{handler: func(p Progress) error { reports = append(reports, p); return nil }}
		for i := 0; i < 4096; i++ {
			peer.data = append(peer.data, makePacket(byte(i), progressPacket("copying"))...)
		}
		peer.data = append(peer.data, makePacket(0, []byte{iOK, 0, 0, 2, 0, 0, 0})...)
		data, err := mc.readPacket()
		if err != nil || data[0] != iOK || len(reports) != 4096 {
			t.Fatalf("reports=%d error=%v", len(reports), err)
		}
		if reports[0].Info != "copying" || reports[0].Thousandths != 50000 || reports[0].MaxStage != 3 {
			t.Fatal(reports[0])
		}
	})
	t.Run("capability_gate", func(t *testing.T) {
		peer, mc := newRWMockConn(0)
		peer.data = makePacket(0, progressPacket("copying"))
		data, err := mc.readPacket()
		if err != nil || data[0] != iERR {
			t.Fatalf("unnegotiated packet swallowed: %v", err)
		}
	})
	valid := progressPacket("copying")
	for n := range len(valid) {
		t.Run(fmt.Sprintf("truncated/%d", n), func(t *testing.T) {
			_, mc := newRWMockConn(0)
			if err := mc.consumeProgress(valid[:n]); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
				t.Fatalf("accepted prefix %d: %v", n, err)
			}
		})
	}
	for _, name := range []string{"fields", "stage_zero", "stage_range", "percent", "null_info", "trailing", "oversized", "sequence"} {
		t.Run(name, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			mc.extCapabilities = progressIndicator
			data := bytes.Clone(valid)
			seq := byte(0)
			switch name {
			case "fields":
				data[3] = 2
			case "stage_zero":
				data[4] = 0
			case "stage_range":
				data[4] = 4
			case "percent":
				putUint24(data[6:9], 100001)
			case "null_info":
				data = append(data[:9], 0xfb)
			case "trailing":
				data = append(data, 0)
			case "oversized":
				data = progressPacket(strings.Repeat("x", 4096))
			case "sequence":
				seq = 7
			}
			peer.data = makePacket(seq, data)
			_, err := mc.readPacket()
			if err == nil || !mc.closed.Load() {
				t.Fatalf("accepted %s: %v", name, err)
			}
		})
	}
}

func TestNativeProgressCallbacks(t *testing.T) {
	for _, kind := range []string{"error", "panic", "error_and_server", "normal"} {
		t.Run(kind, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			mc.extCapabilities = progressIndicator
			response := append(makePacket(1, progressPacket("first")), makePacket(2, progressPacket("second"))...)
			terminal := []byte{iOK, 1, 0, 2, 0, 0, 0}
			if kind == "error_and_server" {
				terminal = []byte{iERR, 0x26, 0x04, '#', '2', '3', '0', '0', '0', 'd', 'u', 'p'}
			}
			response = append(response, makePacket(3, terminal)...)
			peer.queuedReplies = [][]byte{response, makePacket(1, []byte{iOK, 0, 0, 2, 0, 0, 0})}
			conn := nativeSQLConn(t, mc)
			marker := errors.New("callback stopped")
			calls := 0
			result, err := ExecWithProgress(t.Context(), conn, func(p Progress) error {
				calls++
				if kind == "panic" {
					panic("private panic value")
				}
				if kind != "normal" {
					return marker
				}
				return nil
			}, "ALTER TABLE t ADD v INT")
			if kind == "normal" {
				if err != nil || calls != 2 || result == nil {
					t.Fatalf("calls=%d result=%v error=%v", calls, result, err)
				}
			} else {
				if calls != 1 || err == nil {
					t.Fatalf("calls=%d error=%v", calls, err)
				}
				if kind == "panic" {
					var p *ProgressCallbackPanic
					if !errors.As(err, &p) || p.Value != "private panic value" || strings.Contains(err.Error(), "private") {
						t.Fatal(err)
					}
				} else if !errors.Is(err, marker) {
					t.Fatal(err)
				}
				if kind == "error_and_server" {
					var server *MySQLError
					if !errors.As(err, &server) || server.Number != 1062 || result != nil {
						t.Fatal(err)
					}
				} else if result == nil || result.RowsAffectedUnsigned() != 1 {
					t.Fatal("lost successful result")
				}
			}
			if mc.closed.Load() || mc.progress != nil {
				t.Fatal("failed to restore callback state")
			}
			if err := conn.PingContext(t.Context()); err != nil {
				t.Fatalf("did not drain terminal response: %v", err)
			}
		})
	}
}

type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) { return 0, nil }

type invalidReader struct{}

func (invalidReader) Read(p []byte) (int, error) { return len(p) + 1, nil }

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("reader panic") }

func TestNativeStreamWire(t *testing.T) {
	t.Run("maximum_limit", func(t *testing.T) {
		_, mc := newRWMockConn(0)
		stmt := &mysqlStmt{mc: mc, id: 1}
		if err := stmt.writeStreamLongData(0, streamInput{StreamParam{Reader: strings.NewReader("small"), MaxBytes: math.MaxInt64, ChunkBytes: 8}, t.Context()}); err != nil {
			t.Fatal(err)
		}
	})
	for _, size := range []int{0, 1, 7, 8, 9, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			peer, mc := newRWMockConn(42)
			stmt := &mysqlStmt{mc: mc, id: 12, paramCount: 2}
			input := bytes.Repeat([]byte{0xa7}, size)
			if err := stmt.writeExecutePacket([]driver.Value{streamInput{StreamParam{Reader: bytes.NewReader(input), MaxBytes: int64(max(1, size)), ChunkBytes: 8}, t.Context()}, int64(42)}); err != nil {
				t.Fatal(err)
			}
			var actual []byte
			data := peer.written
			chunks := 0
			for len(data) > 0 {
				n := getUint24(data[:3])
				p := data[4 : 4+n]
				if data[3] != 0 {
					t.Fatal("command did not reset sequence")
				}
				if p[0] == comStmtSendLongData {
					chunks++
					if n > 15 || binary.LittleEndian.Uint16(p[5:7]) != 0 {
						t.Fatal("bad chunk")
					}
					actual = append(actual, p[7:]...)
				} else if p[0] != comStmtExecute {
					t.Fatalf("command=%d", p[0])
				}
				data = data[4+n:]
			}
			if !bytes.Equal(actual, input) || chunks != max(1, (size+7)/8) {
				t.Fatalf("chunks=%d bytes=%d", chunks, len(actual))
			}
		})
	}
	for _, name := range []string{"stalled", "invalid_count", "overflow", "bad_ordinary_argument", "reader_panic"} {
		t.Run(name, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			stmt := &mysqlStmt{mc: mc, id: 1, paramCount: 2}
			var reader io.Reader = strings.NewReader("abcdef")
			switch name {
			case "stalled":
				reader = stalledReader{}
			case "invalid_count":
				reader = invalidReader{}
			case "reader_panic":
				reader = panicReader{}
			}
			values := []driver.Value{streamInput{StreamParam{Reader: reader, MaxBytes: 3, ChunkBytes: 2}, t.Context()}, int64(1)}
			if name == "bad_ordinary_argument" {
				values[1] = struct{}{}
			}
			peer.queuedReplies = [][]byte{nil, makePacket(1, []byte{iOK, 0, 0, 2, 0, 0, 0})}
			if name == "stalled" || name == "invalid_count" {
				peer.queuedReplies = peer.queuedReplies[1:]
			}
			if name == "reader_panic" {
				defer func() {
					if recover() == nil || !mc.closed.Load() {
						t.Fatal("reader panic did not retire")
					}
				}()
				stmt.writeExecutePacket(values)
				t.Fatal("missing panic")
			}
			err := stmt.writeExecutePacket(values)
			if err == nil {
				t.Fatal("accepted invalid input")
			}
			if name == "bad_ordinary_argument" && peer.writes != 0 {
				t.Fatal("read/wrote before validation")
			}
			for data := peer.written; len(data) > 0; {
				n := getUint24(data[:3])
				if data[4] == comStmtExecute {
					t.Fatal("executed failed upload")
				}
				data = data[4+n:]
			}
		})
	}
}

func nativeColumn(name string, kind fieldType) []byte {
	var data []byte
	for _, s := range []string{"def", "db", "t", "t", name, name} {
		data = appendLengthEncodedString(data, s)
	}
	return append(data, 12, 63, 0, 20, 0, 0, 0, byte(kind), 0, 0, 0, 0, 0)
}

func TestNativeBulkUnsignedUnits(t *testing.T) {
	peer, mc := newRWMockConn(0)
	mc.capabilities = clientDeprecateEOF
	mc.extCapabilities = clientStmtBulkOperations | clientUnitBulkResult
	prepare := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, []byte{0})...)
	response := makePacket(1, []byte{2})
	response = append(response, makePacket(2, nativeColumn("Id", fieldTypeLongLong))...)
	response = append(response, makePacket(3, nativeColumn("Affected_rows", fieldTypeLongLong))...)
	row := binary.LittleEndian.AppendUint64([]byte{0, 0}, math.MaxUint64)
	row = binary.LittleEndian.AppendUint64(row, 1<<63)
	response = append(response, makePacket(4, row)...)
	response = append(response, makePacket(5, []byte{iEOF, 0, 0, 2, 0, 0, 0})...)
	peer.queuedReplies = [][]byte{prepare, response, nil}
	units, err := BulkExecUnits(t.Context(), nativeSQLConn(t, mc), "INSERT INTO t VALUES (?)", [][]any{{1}})
	if err != nil || len(units) != 1 || units[0].InsertID != math.MaxUint64 || units[0].RowsAffected != 1<<63 {
		t.Fatalf("units=%v error=%v", units, err)
	}
}

func TestNativeOptionsAndPreflight(t *testing.T) {
	for _, n := range []uint32{0, 65536, math.MaxUint32} {
		if NewConfig().Apply(WithServerCursors(n)) == nil {
			t.Fatal("invalid cursor size", n)
		}
	}
	cfg := NewConfig()
	if err := cfg.Apply(WithBulkUnitResults(true), WithProgressReports(true), WithServerCursors(2)); err != nil {
		t.Fatal(err)
	}
	copy := cfg.Clone()
	if !copy.bulkUnitResults || !copy.progressReports || copy.cursorFetchRows != 2 {
		t.Fatal("clone lost native options")
	}
	_, mc := newRWMockConn(0)
	mc.cfg = cfg
	mc.initCapabilities(clientProtocol41, clientStmtBulkOperations|clientUnitBulkResult|progressIndicator)
	if mc.extCapabilities&(clientStmtBulkOperations|clientUnitBulkResult|progressIndicator) != clientStmtBulkOperations|clientUnitBulkResult|progressIndicator {
		t.Fatal("flags not negotiated")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ExecStream(ctx, nil, "SELECT ?", StreamParam{Reader: strings.NewReader("unused")}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := BulkExecUnits(ctx, nil, "INSERT INTO t VALUES (?)", [][]any{{1}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ExecStream(context.Background(), nil, "SELECT 1"); err == nil {
		t.Fatal("nil conn accepted")
	}
}

func TestNativeBulkUnitMalformed(t *testing.T) {
	for _, name := range []string{"column_count", "column_name", "column_type", "null_unit", "short_unit", "too_many_units", "more_results", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			mc.capabilities = clientDeprecateEOF
			mc.extCapabilities = clientStmtBulkOperations | clientUnitBulkResult
			if name == "unavailable" {
				mc.extCapabilities = 0
			}
			prepare := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, []byte{0})...)
			count := byte(2)
			if name == "column_count" {
				count = 1
			}
			response := makePacket(1, []byte{count})
			columnName, kind := "Id", fieldTypeLongLong
			if name == "column_name" {
				columnName = "unrelated"
			}
			if name == "column_type" {
				kind = fieldTypeLong
			}
			response = append(response, makePacket(2, nativeColumn(columnName, kind))...)
			response = append(response, makePacket(3, nativeColumn("Affected_rows", fieldTypeLongLong))...)
			row := make([]byte, 18)
			if name == "null_unit" {
				row[1] = 4
			}
			if name == "short_unit" {
				row = row[:17]
			}
			response = append(response, makePacket(4, row)...)
			seq := byte(5)
			if name == "too_many_units" {
				response = append(response, makePacket(seq, row)...)
				seq++
			}
			status := statusInAutocommit
			if name == "more_results" {
				status |= statusMoreResultsExists
			}
			response = append(response, makePacket(seq, []byte{iEOF, 0, 0, byte(status), byte(status >> 8), 0, 0})...)
			peer.queuedReplies = [][]byte{prepare, response, nil}
			units, err := BulkExecUnits(t.Context(), nativeSQLConn(t, mc), "INSERT INTO t VALUES (?)", [][]any{{1}})
			if err == nil || units != nil || errors.Is(err, driver.ErrBadConn) {
				t.Fatalf("units=%v error=%v", units, err)
			}
			if name == "unavailable" {
				if peer.writes != 0 || !errors.Is(err, ErrUnsupportedCapability) {
					t.Fatal(err)
				}
			} else if !mc.closed.Load() {
				t.Fatal("malformed unit stream remained reusable")
			}
		})
	}
}

func TestNativeNoReplay(t *testing.T) {
	for _, kind := range []string{"stream", "progress", "units"} {
		t.Run(kind, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			mc.extCapabilities = progressIndicator | clientStmtBulkOperations | clientUnitBulkResult
			peer.writeErrors = map[int]error{1: io.ErrClosedPipe}
			conn := nativeSQLConn(t, mc)
			var err error
			switch kind {
			case "stream":
				_, err = ExecStream(t.Context(), conn, "INSERT INTO t VALUES (?)", StreamParam{Reader: strings.NewReader("once")})
			case "progress":
				_, err = ExecWithProgress(t.Context(), conn, func(Progress) error { return nil }, "ALTER TABLE t ADD v INT")
			case "units":
				_, err = BulkExecUnits(t.Context(), conn, "INSERT INTO t VALUES (?)", [][]any{{1}})
			}
			if err == nil || errors.Is(err, driver.ErrBadConn) || peer.writes != 1 || !mc.closed.Load() {
				t.Fatalf("writes=%d error=%v", peer.writes, err)
			}
		})
	}
}

func FuzzNativeProgress(f *testing.F) {
	f.Add(progressPacket("copying"))
	f.Add([]byte{0xff, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			return
		}
		_, mc := newRWMockConn(0)
		_ = mc.consumeProgress(data)
	})
}
