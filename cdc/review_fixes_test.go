// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"
	"strings"
	"testing"

	mariadb "github.com/aantich/go-mariadb"
)

// eventAt keeps fault transcripts CRC-valid: the failure under test is the
// physical coordinate rather than an unrelated checksum mismatch.
func eventAt(typ byte, body []byte, position uint32, flags uint16) []byte {
	raw := wireEvent(typ, body, true)
	binary.LittleEndian.PutUint32(raw[13:], position)
	binary.LittleEndian.PutUint16(raw[17:], flags)
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	return raw
}

func rotateBody(file string, position uint64) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint64(body, position)
	return append(body, file...)
}

func TestCDCFileCoordinates(t *testing.T) {
	for _, test := range []struct {
		name     string
		position uint32
		previous bool
	}{
		{"below requested start", 900, false},
		{"equal to previous event", 1050, false},
		{"below completed group", 1099, true},
		{"equal completed group", 1100, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits, _ := (Limits{}).normalize()
			d := newDecoder(limits, Resume{File: "bin.000001", Position: 1000})
			for _, raw := range [][]byte{
				eventAt(RotateEvent, rotateBody("bin.000001", 4), 0, 0x20),
				eventAt(FormatEvent, formatBody(true), 0, 0x20),
				eventAt(GTIDListEvent, make([]byte, 4), 0, 0x20),
				eventAt(HeartbeatEvent, nil, 1000, 0),
				eventAt(GTIDEvent, gtidBody(1, 1), 1050, 0),
			} {
				if _, err := d.decode(raw, false); err != nil {
					t.Fatal("valid start/context rejected", err)
				}
			}
			var typ byte = QueryEvent
			body := queryBody("CREATE TABLE t(id INT)")
			if test.previous {
				first, err := d.decode(eventAt(typ, body, 1100, 0), false)
				if err != nil || first.Checkpoint == nil {
					t.Fatal("valid checkpoint rejected", err)
				}
				typ, body = GTIDEvent, gtidBody(2, 1)
			}
			sequence, completed := d.sequence, d.completed.String()
			if event, err := d.decode(eventAt(typ, body, test.position, 0), true); !errors.Is(err, ErrMalformed) || event != nil {
				t.Fatal("backward/equal coordinate accepted", event, err)
			}
			if d.sequence != sequence || d.completed.String() != completed {
				t.Fatal("invalid coordinate published completed progress")
			}
		})
	}
	t.Run("rollover inside group", func(t *testing.T) {
		d := testDecoder(t)
		for _, raw := range [][]byte{
			eventAt(GTIDEvent, gtidBody(1, 4), math.MaxUint32-200, 0),
			eventAt(QueryEvent, queryBody("BEGIN"), math.MaxUint32-100, 0),
		} {
			if _, err := d.decode(raw, false); err != nil {
				t.Fatal(err)
			}
		}
		if event, err := d.decode(eventAt(XIDEvent, make([]byte, 8), 100, 0), true); !errors.Is(err, ErrMalformed) || event != nil || d.sequence != 0 {
			t.Fatal("uint32 rollover produced a checkpoint", event, err)
		}
	})
	t.Run("artificial completion", func(t *testing.T) {
		d := testDecoder(t)
		decodeEvent(t, d, GTIDEvent, gtidBody(1, 1))
		if event, err := d.decode(eventAt(QueryEvent, queryBody("DDL"), uint32(d.position+100), 0x20), true); !errors.Is(err, ErrMalformed) || event != nil || d.sequence != 0 {
			t.Fatal("artificial event completed a group", event, err)
		}
	})
	t.Run("cached intermediate events", func(t *testing.T) {
		d := testDecoder(t)
		decodeEvent(t, d, GTIDEvent, gtidBody(1, 4))
		for _, raw := range [][]byte{
			eventAt(AnnotateEvent, []byte("INSERT INTO t VALUES(7,'old')"), 0, 0),
			eventAt(TableMapEvent, tableBody(), 0, 0),
			eventAt(WriteRowsV1, rowBody(WriteRowsV1), 0, 0),
		} {
			if event, err := d.decode(raw, false); err != nil || event.Checkpoint != nil {
				t.Fatal("cached event rejected or checkpointed", event, err)
			}
		}
		if event := decodeEvent(t, d, XIDEvent, make([]byte, 8)); event.Checkpoint == nil {
			t.Fatal("explicit complete-group offset rejected")
		}
	})
	t.Run("cached bytes cannot hide rollover", func(t *testing.T) {
		d := testDecoder(t)
		if _, err := d.decode(eventAt(GTIDEvent, gtidBody(1, 4), math.MaxUint32-10, 0), false); err != nil {
			t.Fatal(err)
		}
		if event, err := d.decode(eventAt(AnnotateEvent, []byte("INSERT"), 0, 0), false); !errors.Is(err, ErrMalformed) || event != nil || d.sequence != 0 {
			t.Fatal("cached event hid rollover", event, err)
		}
	})
	t.Run("rotate resets only for another file", func(t *testing.T) {
		d := testDecoder(t)
		decodeEvent(t, d, GTIDEvent, gtidBody(1, 1))
		decodeEvent(t, d, QueryEvent, queryBody("DDL"))
		before := d.position
		decodeEvent(t, d, RotateEvent, rotateBody(d.file, 4))
		if d.position != before || d.boundary != before {
			t.Fatal("same-file bootstrap lowered progress")
		}
		decodeEvent(t, d, RotateEvent, rotateBody("bin.000002", 4))
		if d.position != 4 || d.boundary != 4 {
			t.Fatal("new-file position did not reset")
		}
		decodeEvent(t, d, FormatEvent, formatBody(true))
		decodeEvent(t, d, GTIDEvent, gtidBody(2, 1))
		event := decodeEvent(t, d, QueryEvent, queryBody("DDL"))
		if event.Checkpoint.Resume().File != "bin.000002" || event.Position != d.position || event.Position <= 4 {
			t.Fatal("new-file position did not reset", event.Checkpoint.Resume())
		}
	})
	t.Run("GTID ignores stale diagnostic file offset", func(t *testing.T) {
		limits, _ := (Limits{}).normalize()
		d := newDecoder(limits, Resume{File: "bin.000001", Position: math.MaxUint32, UseGTID: true})
		decodeEvent(t, d, RotateEvent, rotateBody("bin.000001", 4))
		decodeEvent(t, d, FormatEvent, formatBody(true))
		decodeEvent(t, d, GTIDEvent, gtidBody(1, 1))
		if event := decodeEvent(t, d, QueryEvent, queryBody("DDL")); event.Checkpoint == nil {
			t.Fatal("GTID start inherited stale file bound")
		}
	})
}

func TestCDCExpansionIntegrity(t *testing.T) {
	for _, sql := range []string{"", strings.Repeat("x", 70000)} {
		name := "empty SQL"
		if sql != "" {
			name = "multi-byte expansion length"
		}
		t.Run(name, func(t *testing.T) {
			d := testDecoder(t)
			decodeEvent(t, d, GTIDEvent, gtidBody(1, 4))
			event := decodeEvent(t, d, 165, compressedBody(queryBody(sql), 15))
			if !bytes.Equal(event.Data.(Query).SQL, []byte(sql)) || event.Checkpoint != nil {
				t.Fatal("valid expansion size changed payload or group boundary")
			}
		})
	}
	for _, fault := range []string{"valid", "short advertised", "long advertised", "truncated footer", "corrupt footer", "trailing bytes", "second stream"} {
		t.Run(fault, func(t *testing.T) {
			d := testDecoder(t)
			decodeEvent(t, d, GTIDEvent, gtidBody(1, 1))
			plain := queryBody("CREATE TABLE t(id INT)")
			body := compressedBody(plain, 15)
			switch fault {
			case "short advertised":
				body[16]--
			case "long advertised":
				body[16]++
			case "truncated footer":
				body = body[:len(body)-1]
			case "corrupt footer":
				body[len(body)-1] ^= 1
			case "trailing bytes":
				body = append(body, 0)
			case "second stream":
				body = append(body, body[17:]...)
			}
			event, err := d.decode(wireAfter(d, 165, body, true), false)
			if fault != "valid" {
				if !errors.Is(err, ErrMalformed) || event != nil || d.sequence != 0 {
					t.Fatal("bad expansion published progress", event, err)
				}
				return
			}
			if err != nil || event.Checkpoint == nil || !bytes.Equal(event.Data.(Query).SQL, plain[15:]) {
				t.Fatal("valid expansion changed query", event, err)
			}
			retained := event.Data.(Query).SQL
			decodeEvent(t, d, GTIDEvent, gtidBody(2, 1))
			decodeEvent(t, d, 165, compressedBody(queryBody("DROP TABLE t"), 15))
			if !bytes.Equal(retained, plain[15:]) {
				t.Fatal("next expansion overwrote a retained event")
			}
		})
	}
}

func TestCDCSourceEndValidation(t *testing.T) {
	for _, complete := range []bool{false, true} {
		for _, malformed := range []bool{false, true} {
			name := "partial"
			if complete {
				name = "complete"
			}
			if malformed {
				name += "/malformed EOF"
			} else {
				name += "/valid EOF"
			}
			t.Run(name, func(t *testing.T) {
				s, transport := streamFixture(t, false)
				defer s.Close()
				count := 2
				if complete {
					count = 3
				}
				transport.packets = transport.packets[:count]
				want := error(io.EOF)
				if malformed {
					transport.readErr, want = mariadb.ErrMalformPkt, mariadb.ErrMalformPkt
				} else if !complete {
					want = ErrMalformed
				}
				for range count {
					if _, err := s.Next(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				_, err := s.Next(t.Context())
				if !errors.Is(err, want) {
					t.Fatal("source end lost its validity/group state", err)
				}
				if _, again := s.Next(t.Context()); again != err {
					t.Fatal("stream error was not sticky", err, again)
				}
				if (s.LatestCheckpoint() != nil) != complete || s.AcknowledgedCheckpoint() != nil || transport.acks != 0 {
					t.Fatal("source end advanced/acknowledged progress")
				}
			})
		}
	}
}
