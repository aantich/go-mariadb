// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

type interruptedUpload struct{ first bool }

type panicNativeValuer struct{}

func (panicNativeValuer) Value() (driver.Value, error) { panic("valuer panic") }

func TestNativeValuerPanicCleanup(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	mc := &mysqlConn{buf: newBuffer(), cfg: NewConfig(), netConn: client, rawConn: client, closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF, extCapabilities: clientStmtBulkOperations | clientUnitBulkResult}
	mc.startWatcher()
	conn := nativeSQLConn(t, mc)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if readFaultCommand(peer) != nil {
			return
		}
		response := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, []byte{0})...)
		peer.Write(response)
	}()
	defer func() {
		if value := recover(); value != "valuer panic" || !mc.closed.Load() {
			t.Errorf("panic=%v closed=%v", value, mc.closed.Load())
		}
		peer.Close()
		<-finished
	}()
	// No peer reads COM_STMT_CLOSE. Retirement must precede cleanup, without
	// depending on cancellation to release a blocked cleanup write.
	BulkExecUnits(t.Context(), conn, "INSERT INTO t VALUES (?)", [][]any{{panicNativeValuer{}}})
}

func (r *interruptedUpload) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return copy(p, "ab"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestNativeStreamCancellation(t *testing.T) {
	for _, phase := range []string{"prepare_write", "prepare_reply", "upload_write", "upload_midway", "execute_write", "execute_reply", "reset_reply", "close_write"} {
		t.Run(phase, func(t *testing.T) {
			client, peer := net.Pipe()
			defer peer.Close()
			mc := &mysqlConn{buf: newBuffer(), cfg: NewConfig(), netConn: client, rawConn: client, closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
			mc.startWatcher()
			conn := nativeSQLConn(t, mc)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if phase == "prepare_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "prepare_reply" {
					return
				}
				response := append(makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}), makePacket(2, []byte{0})...)
				if _, err := peer.Write(response); err != nil || phase == "upload_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "upload_midway" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "execute_write" || phase == "reset_reply" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "execute_reply" {
					return
				}
				peer.Write(makePacket(1, []byte{iOK, 1, 0, 2, 0, 0, 0}))
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			var reader io.Reader = strings.NewReader("abcd")
			if phase == "reset_reply" {
				reader = &interruptedUpload{}
			}
			_, err := ExecStream(ctx, conn, "INSERT INTO t VALUES (?)", StreamParam{Reader: reader, ChunkBytes: 2})
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || !mc.closed.Load() {
				t.Fatalf("phase=%s closed=%v error=%v", phase, mc.closed.Load(), err)
			}
			peer.Close()
			<-finished
		})
	}
}

func TestNativeCursorCancellation(t *testing.T) {
	for _, phase := range []string{"execute_write", "open_reply", "fetch_write", "fetch_reply", "drain", "reset_write", "reset_ack"} {
		t.Run(phase, func(t *testing.T) {
			client, peer := net.Pipe()
			defer peer.Close()
			cfg := NewConfig()
			cfg.cursorFetchRows = 2
			mc := &mysqlConn{buf: newBuffer(), cfg: cfg, netConn: client, rawConn: client, closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
			mc.startWatcher()
			defer mc.close()
			stmt := &mysqlStmt{mc: mc, id: 9}
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if phase == "execute_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "open_reply" {
					return
				}
				response := append(makePacket(1, []byte{1}), makePacket(2, nativeColumn("v", fieldTypeLong))...)
				response = append(response, makePacket(3, []byte{iEOF, 0, 0, byte(statusCursorExists), 0, 0, 0})...)
				if _, err := peer.Write(response); err != nil {
					return
				}
				if phase == "fetch_write" || phase == "reset_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "fetch_reply" || phase == "reset_ack" {
					return
				}
				if phase == "drain" {
					peer.Write(makePacket(1, []byte{0, 0, 1, 0, 0, 0}))
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			rows, err := stmt.QueryContext(ctx, nil)
			if err == nil {
				if phase == "reset_write" || phase == "reset_ack" {
					err = rows.Close()
				} else {
					err = rows.Next(make([]driver.Value, 1))
					if err == nil {
						err = rows.Close()
					}
				}
			}
			if !errors.Is(err, context.DeadlineExceeded) || !mc.closed.Load() {
				t.Fatalf("phase=%s closed=%v error=%v", phase, mc.closed.Load(), err)
			}
			peer.Close()
			<-finished
		})
	}
}

func TestNativeCursorMalformedFetch(t *testing.T) {
	for _, name := range []string{"empty_open_batch", "no_flags", "both_flags", "more_results", "extra_row", "truncated_row", "short_open_batch"} {
		t.Run(name, func(t *testing.T) {
			peer, mc := newRWMockConn(0)
			mc.capabilities = clientDeprecateEOF
			stmt := &mysqlStmt{mc: mc, id: 1}
			rows := &cursorRows{binaryRows: binaryRows{mysqlRows{mc: mc, rs: resultSet{columns: []mysqlField{{fieldType: fieldTypeLong}}}}}, owner: mc, stmt: stmt, fetch: 1, pending: true}
			status := statusCursorExists
			switch name {
			case "no_flags":
				status = 0
			case "both_flags":
				status |= statusLastRowSent
			case "more_results":
				status |= statusMoreResultsExists
			}
			seq := byte(0)
			if name == "extra_row" || name == "truncated_row" || name == "short_open_batch" {
				row := []byte{0, 0, 1, 0, 0, 0}
				if name == "truncated_row" {
					row = row[:3]
				}
				peer.data = makePacket(seq, row)
				seq++
				if name == "extra_row" {
					peer.data = append(peer.data, makePacket(seq, row)...)
					seq++
				}
				if name == "short_open_batch" {
					rows.fetch = 2
				}
			}
			peer.data = append(peer.data, makePacket(seq, []byte{iEOF, 0, 0, byte(status), byte(status >> 8), 0, 0})...)
			var err error
			for range 3 {
				err = rows.Next(make([]driver.Value, 1))
				if err != nil {
					break
				}
			}
			if err == nil || err == io.EOF || !mc.closed.Load() {
				t.Fatalf("accepted malformed fetch %s: %v", name, err)
			}
		})
	}
}

// generatedStream never allocates a payload-sized slice. This resource test
// observes total allocation, not elapsed time, and checks that its positive
// materialization control would fail the same staging-memory bound.
type generatedStream struct{ remaining int64 }

func (r *generatedStream) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.remaining))
	clear(p[:n])
	r.remaining -= int64(n)
	return n, nil
}

type uploadSink struct {
	mockConn
	bytes   int64
	largest int
}

func (c *uploadSink) Write(p []byte) (int, error) {
	c.bytes += int64(len(p))
	c.largest = max(c.largest, len(p))
	return len(p), nil
}

func TestNativeStreamBoundedStaging(t *testing.T) {
	const payload = 16 << 20
	_, mc := newRWMockConn(0)
	sink := &uploadSink{}
	mc.netConn = sink
	stmt := &mysqlStmt{mc: mc, id: 1}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := stmt.writeStreamLongData(0, streamInput{StreamParam{Reader: &generatedStream{payload}, MaxBytes: payload, ChunkBytes: 64 << 10}, t.Context()})
	runtime.ReadMemStats(&after)
	if err != nil || sink.bytes < payload || sink.largest > 64<<10+11 || after.TotalAlloc-before.TotalAlloc > 2<<20 {
		t.Fatalf("staging allocation=%d largest=%d error=%v", after.TotalAlloc-before.TotalAlloc, sink.largest, err)
	}
	runtime.ReadMemStats(&before)
	data, err := io.ReadAll(&generatedStream{payload})
	runtime.ReadMemStats(&after)
	if err != nil || len(data) != payload || after.TotalAlloc-before.TotalAlloc <= 2<<20 {
		t.Fatal("materialization control did not exceed bound")
	}
	runtime.KeepAlive(data)
}
