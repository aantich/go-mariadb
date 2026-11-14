// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestColumnAttributeDiscardValidation(t *testing.T) {
	attribute := func(kind uint64, value string) []byte {
		return appendLengthEncodedString(appendLengthEncodedInteger(nil, kind), value)
	}
	valid := append(attribute(0, "uuid"), attribute(1, "json")...)
	valid = append(valid, attribute(64, "future")...)
	for _, retain := range []bool{false, true} {
		t.Run(fmt.Sprint(retain), func(t *testing.T) {
			attributes, native, format, err := decodeColumnAttributes(valid, retain)
			if err != nil {
				t.Fatal(err)
			}
			if retain {
				if len(attributes) != 3 || native != "UUID" || format != "json" {
					t.Fatalf("retained attributes: %v/%s/%s", attributes, native, format)
				}
			} else if attributes != nil || native != "" || format != "" {
				t.Fatal("discard decoder materialized returned values")
			}
			for _, kind := range []uint64{0, 1, 63, 64, 1024, ^uint64(0)} {
				duplicate := append(attribute(kind, "a"), attribute(kind, "b")...)
				if _, _, _, err := decodeColumnAttributes(duplicate, retain); !errors.Is(err, ErrMalformPkt) {
					t.Fatalf("duplicate kind %d accepted", kind)
				}
			}
			var many []byte
			for kind := range uint64(1024) {
				many = append(many, attribute(kind, "")...)
			}
			if _, _, _, err := decodeColumnAttributes(many, retain); err != nil {
				t.Fatal("maximum attribute count rejected", err)
			}
			many = append(many, attribute(1024, "")...)
			if _, _, _, err := decodeColumnAttributes(many, retain); !errors.Is(err, ErrMalformPkt) {
				t.Fatal("attribute count limit ignored")
			}
		})
	}
}

func TestExtendedColumnDiscardBoundaries(t *testing.T) {
	metadata := append([]byte{0}, appendLengthEncodedString(nil, "uuid")...)
	column := extendedColumn(metadata)
	for prefix := range len(column) {
		if err := validateExtendedColumn(column[:prefix]); err == nil {
			t.Fatalf("accepted incomplete definition, prefix %d", prefix)
		}
	}
	if err := validateExtendedColumn(append(column, 0)); err != nil {
		t.Fatal("optional default-value tail rejected", err)
	}
	for _, deprecated := range []bool{false, true} {
		t.Run(fmt.Sprint(deprecated), func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			mc.extCapabilities = clientExtendedMetadata
			if deprecated {
				mc.capabilities = clientDeprecateEOF
			}
			mc.cfg.metadataByteLimit = len(column) * 2
			conn.data = append(makePacket(0, column), makePacket(1, column)...)
			next := uint8(2)
			if !deprecated {
				conn.data = append(conn.data, makePacket(next, []byte{iEOF, 0, 0, 2, 0})...)
				next++
			}
			ok := []byte{iOK, 0, 0, 2, 0, 0, 0}
			conn.data = append(conn.data, makePacket(next, ok)...)
			if err := mc.skipColumns(2); err != nil {
				t.Fatal(err)
			}
			packet, err := mc.readPacket()
			if err != nil || !bytes.Equal(packet, ok) {
				t.Fatalf("discard crossed command boundary: %x %v", packet, err)
			}
		})
	}
}

func TestExtendedColumnDiscardRetiresMalformedStream(t *testing.T) {
	duplicate := []byte{0, 1, 'a', 0, 1, 'b'}
	for _, column := range [][]byte{extendedColumn(duplicate), extendedColumn([]byte{0xfc}), {0xfc}} {
		conn, mc := newRWMockConn(0)
		mc.extCapabilities, mc.capabilities = clientExtendedMetadata, clientDeprecateEOF
		conn.data = makePacket(0, column)
		if err := mc.skipColumns(1); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
			t.Fatalf("malformed discarded definition: %x %v", column, err)
		}
	}
	conn, mc := newRWMockConn(0)
	mc.extCapabilities, mc.capabilities = clientExtendedMetadata, clientDeprecateEOF
	column := extendedColumn(nil)
	mc.cfg.metadataByteLimit = len(column)*2 - 1
	conn.data = append(makePacket(0, column), makePacket(1, column)...)
	if err := mc.skipColumns(2); !errors.Is(err, ErrPktTooLarge) || !mc.closed.Load() {
		t.Fatalf("discard metadata byte limit: %v", err)
	}
}

// Discard and retain paths share their attribute/fixed-tail validators. Fuzz
// seeds run as ordinary correctness checks; no timed campaign is required here.
func FuzzDiscardedExtendedColumn(f *testing.F) {
	f.Add(extendedColumn([]byte{0, 4, 'u', 'u', 'i', 'd'}))
	f.Add([]byte{0xfc})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) <= 64<<10 {
			_ = validateExtendedColumn(data)
		}
	})
}
