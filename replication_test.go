// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

func TestMariaDBGTIDSet(t *testing.T) {
	for _, text := range []string{"0-1-1", "4294967295-4294967295-18446744073709551615", "2-3-4,0-1-9", ""} {
		t.Run("valid/"+text, func(t *testing.T) {
			set, err := ParseGTIDSet(text)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(set)
			if err != nil {
				t.Fatal(err)
			}
			var restored GTIDSet
			if err = json.Unmarshal(data, &restored); err != nil || restored.String() != set.String() {
				t.Fatal("JSON roundtrip", err)
			}
			entries := set.Entries()
			if len(entries) > 0 {
				entries[0].Sequence = 0
				if set.Entries()[0].Sequence == 0 {
					t.Fatal("Entries aliases immutable state")
				}
			}
		})
	}
	for _, text := range []string{"0-1-0", "-1-2-3", "0-1-1,0-2-2", "1-2", "1-2-3-4", "1-2-+3", "1-2-3 ", "4294967296-1-1", "1-4294967296-1", "1-2-18446744073709551616", strings.Repeat("x", 65536), strings.Repeat("1-2-3,", 4096)} {
		t.Run("invalid/"+text[:min(len(text), 50)], func(t *testing.T) {
			if _, err := ParseGTIDSet(text); err == nil {
				t.Fatal("invalid progress accepted")
			}
		})
	}
	set, _ := ParseGTIDSet("0-1-10,2-2-5")
	next, err := set.Advance(GTID{0, 3, 11})
	if err != nil || next.String() != "0-3-11,2-2-5" || set.String() != "0-1-10,2-2-5" {
		t.Fatal("advance mutated history", err)
	}
	for _, g := range []GTID{{0, 4, 10}, {0, 3, 9}, {3, 1, 0}} {
		if _, err := set.Advance(g); err == nil {
			t.Fatal("nonadvancing state accepted")
		}
	}
	entries := make([]GTID, 4096)
	for i := range entries {
		entries[i] = GTID{Domain: uint32(i), Server: 1, Sequence: 1}
	}
	full, err := NewGTIDSet(entries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = full.Advance(GTID{4096, 1, 1}); err == nil {
		t.Fatal("domain budget bypassed")
	}
	// Constructors and advances must preserve the parser's text budget, so
	// every accepted durable state can be restored through JSON/TextUnmarshaler.
	for i := range entries {
		entries[i].Server = math.MaxUint32
		entries[i].Sequence = math.MaxUint64
	}
	if _, err := NewGTIDSet(entries); err == nil {
		t.Fatal("constructed text budget bypassed")
	}
	for _, set := range []GTIDSet{full, next} {
		if set.encodedBytes != len(set.String()) {
			t.Fatal("cached canonical length differs")
		}
	}
}

func replicationTestConn(mc *mysqlConn, semi bool, limit int) *ReplicationConn {
	return &ReplicationConn{mc: mc, gate: make(chan struct{}, 1), semi: semi, limit: limit, nextSeq: mc.sequence}
}
func TestReplicationPacketPolicy(t *testing.T) {
	for _, kind := range []string{"sequence", "size before body", "semisync framing", "reset allowed", "ordinary reset rejected", "canceled waiter"} {
		t.Run(kind, func(t *testing.T) {
			mock, mc := newRWMockConn(1)
			r := replicationTestConn(mc, strings.Contains(kind, "semi") || kind == "reset allowed", 64)
			payload := append([]byte{0}, make([]byte, 19)...)
			seq := byte(1)
			want := error(nil)
			switch kind {
			case "sequence":
				seq = 4
				want = ErrPktSync
			case "size before body":
				mock.data = []byte{100, 0, 0, 1}
				want = ErrPktTooLarge
			case "semisync framing":
				payload = []byte{0, 0xef, 2}
				want = ErrMalformPkt
			case "reset allowed":
				r.nextSeq = 13
				seq = 2
				payload = append([]byte{0, 0xef, 0}, make([]byte, 19)...)
			case "ordinary reset rejected":
				r.nextSeq = 13
				seq = 2
				want = ErrPktSync
			case "canceled waiter":
				ctx, cancel := context.WithCancel(t.Context())
				r.gate <- struct{}{}
				cancel()
				if _, err := r.Read(ctx); !errors.Is(err, context.Canceled) || mc.closed.Load() {
					t.Fatal("waiting cancellation poisoned connection", err)
				}
				<-r.gate
			}
			if kind != "size before body" {
				mock.data = makePacket(seq, payload)
			}
			mock.maxReads = 2
			_, err := r.Read(t.Context())
			if want == nil && err != nil || want != nil && !errors.Is(err, want) {
				t.Fatal(kind, err)
			}
			if want != nil && !mc.closed.Load() {
				t.Fatal("failed replication transport remained open")
			}
		})
	}
}

func TestReplicationEOFValidation(t *testing.T) {
	// COM_BINLOG_DUMP always uses the classic five-byte EOF, including when
	// ordinary SQL results negotiate CLIENT_DEPRECATE_EOF. Truncated EOF and
	// OK-shaped packets must not turn an interrupted source into clean EOF.
	for _, deprecate := range []bool{false, true} {
		for _, size := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9} {
			t.Run(fmt.Sprintf("deprecate=%t/bytes=%d", deprecate, size), func(t *testing.T) {
				mock, mc := newRWMockConn(1)
				if deprecate {
					mc.capabilities |= clientDeprecateEOF
				}
				body := make([]byte, size)
				body[0] = iEOF
				mock.data, mock.maxReads = makePacket(1, body), 2
				r := replicationTestConn(mc, false, 128)
				_, err := r.Read(t.Context())
				want := ErrMalformPkt
				if size == 5 {
					want = io.EOF
				}
				if !errors.Is(err, want) || !mc.closed.Load() {
					t.Fatal("terminator did not retire transport with expected status", err)
				}
				if _, again := r.Read(t.Context()); again == nil {
					t.Fatal("retired transport accepted another read")
				}
			})
		}
	}
}
func TestReplicationACKPreservesBufferedCompression(t *testing.T) {
	mock, mc := newRWMockConn(1)
	mc.compress = true
	mc.compIO = newCompIO(mc)
	packet := makePacket(2, append([]byte{0, 0xef, 1}, make([]byte, 19)...))
	mc.compIO.buff.Write(packet)
	mc.buf.buf = []byte("unread envelope")
	r := replicationTestConn(mc, true, 128)
	readIO := mc.compIO
	if err := r.Acknowledge(t.Context(), "bin.000001", 123); err != nil {
		t.Fatal(err)
	}
	if mc.compIO != readIO || !bytes.Equal(readIO.buff.Bytes(), packet) || string(mc.buf.buf) != "unread envelope" || len(mock.written) == 0 {
		t.Fatal("ACK consumed buffered replication input")
	}
	event, err := r.Read(t.Context())
	if err != nil || len(event.Data) != 19 || !event.AckRequested {
		t.Fatal("buffered event after receipt", err)
	}
}
func TestReplicationCancellation(t *testing.T) {
	for _, closeInstead := range []bool{false, true} {
		t.Run(map[bool]string{false: "context", true: "close"}[closeInstead], func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			cfg := NewConfig()
			mc := &mysqlConn{cfg: cfg, netConn: client, rawConn: client, closech: make(chan struct{}), buf: newBuffer()}
			mc.startWatcher()
			defer mc.cleanup()
			r := replicationTestConn(mc, false, 64)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			returned := make(chan error, 1)
			go func() { _, err := r.Read(ctx); returned <- err }()
			server.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := server.Write([]byte{1, 0}); err != nil {
				t.Fatal("read did not start", err)
			}
			if closeInstead {
				r.Close()
			} else {
				cancel()
			}
			select {
			case err := <-returned:
				if err == nil || (!closeInstead && !errors.Is(err, context.Canceled)) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("replication read did not stop")
			}
		})
	}
}
func TestReplicationLocalValidation(t *testing.T) {
	for _, options := range []ReplicationOptions{{}, {ServerID: 1, File: "bin", Position: math.MaxUint32 + 1}, {ServerID: 1, File: "bin\x00bad", Position: 4}, {ServerID: 1, File: "bin", Position: 4, MaxEventBytes: 18}, {ServerID: 1, UseGTID: true, Heartbeat: time.Nanosecond}} {
		cfg := NewConfig()
		cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) {
			t.Error("invalid options dialed")
			return nil, errors.New("unexpected dial")
		}
		if _, err := OpenReplication(t.Context(), cfg, options); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
