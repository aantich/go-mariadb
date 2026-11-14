// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestCDCValues(t *testing.T) {
	tests := []struct {
		name   string
		column Column
		raw    []byte
		want   any
	}{
		{"signed", Column{Type: 3, SignednessKnown: true}, []byte{255, 255, 255, 255}, int64(-1)},
		{"unsigned", Column{Type: 8, SignednessKnown: true, Unsigned: true}, []byte{255, 255, 255, 255, 255, 255, 255, 255}, ^uint64(0)},
		{"unknown signedness", Column{Type: 3}, []byte{255, 255, 255, 255}, Integer{0xffffffff, 4}},
		{"decimal", Column{Type: 246, Metadata: 5<<8 | 2}, []byte{0x80, 0x7b, 0x2d}, "123.45"},
		{"negative decimal", Column{Type: 246, Metadata: 5<<8 | 2}, []byte{0x7f, 0x84, 0xd2}, "-123.45"},
		{"fractional decimal", Column{Type: 246, Metadata: 4<<8 | 2}, []byte{0x80, 1}, "0.01"},
		{"bit", Column{Type: 16, Metadata: 10}, []byte{2, 1}, uint64(513)},
		{"set", Column{Type: 248, Metadata: 3}, []byte{1, 2, 3}, uint64(0x030201)},
		{"enum", Column{Type: 247, Metadata: 2}, []byte{44, 1}, uint64(300)},
		{"year zero", Column{Type: 13}, []byte{0}, 0},
		{"year", Column{Type: 13}, []byte{126}, 2026},
		{"float", Column{Type: 4}, []byte{0, 0, 0xc0, 0x3f}, float32(1.5)},
		{"bytes", Column{Type: 252, Metadata: 1}, []byte{0, 255}, []byte{0, 255}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := (Cell{Present: true, Raw: test.raw}).Decode(test.column)
			if err != nil || !reflect.DeepEqual(value, test.want) {
				t.Fatalf("value %#v, want %#v: %v", value, test.want, err)
			}
		})
	}
	for _, column := range []Column{{Type: 246, Metadata: 0}, {Type: 16, Metadata: 65}, {Type: 19, Metadata: 7}, {Type: 247, Metadata: 8}, {Type: 3}, {Type: 4}} {
		if _, err := (Cell{Present: true}).Decode(column); err == nil {
			t.Fatal("invalid cell accepted", column)
		}
	}
	if _, err := (Cell{}).Decode(Column{Type: 3}); err == nil {
		t.Fatal("absent treated as NULL")
	}
	if value, err := (Cell{Present: true, Null: true}).Decode(Column{Type: 3}); err != nil || value != nil {
		t.Fatal("NULL", err)
	}
}
func TestCDCTemporal(t *testing.T) {
	for _, fractional := range []bool{false, true} {
		precision := 0
		v := uint64(0x800000 - ((1 << 12) + (2 << 6) + 3))
		if fractional {
			precision = 2
			v--
		}
		raw := []byte{byte(v >> 16), byte(v >> 8), byte(v)}
		if fractional {
			raw = append(raw, 244)
		}
		value, err := decodeTemporal(raw, 19, precision)
		if err != nil || !value.Negative || value.Hour != 1 || value.Minute != 2 || value.Second != 3 || value.Microsecond != map[bool]int{false: 0, true: 120000}[fractional] {
			t.Fatal("negative TIME2", value, err)
		}
	}
	stamp := make([]byte, 7)
	binary.BigEndian.PutUint32(stamp, 0xffffffff)
	copy(stamp[4:], []byte{15, 66, 63})
	value, err := decodeTemporal(stamp, 17, 6)
	if err != nil || value.UnixSeconds != 0xffffffff || value.Microsecond != 999999 {
		t.Fatal(value, err)
	}
	v := uint64(0x8000000000) | uint64(2026*13+11)<<22 | 12<<17 | 1<<12 | 23<<6 | 45
	raw := []byte{byte(v >> 32), byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v), 1, 0xe2, 0x40}
	value, err = decodeTemporal(raw, 18, 6)
	if err != nil || value.Year != 2026 || value.Month != 11 || value.Day != 12 || value.Microsecond != 123456 {
		t.Fatal(value, err)
	}
	if _, err = decodeTemporal([]byte{0, 0, 0}, 10, 0); err != nil {
		t.Fatal("zero date normalized or rejected", err)
	}
	if _, err = decodeTemporal([]byte{0x80, 0, 0, 11}, 19, 1); err == nil {
		t.Fatal("invalid fractional precision accepted")
	}
}

func FuzzCDCValues(f *testing.F) {
	f.Add(byte(246), uint16(5<<8|2), []byte{0x80, 0x7b, 0x2d})
	f.Add(byte(19), uint16(2), []byte{0x80, 0, 0, 0})
	f.Add(byte(3), uint16(0), []byte{1, 2, 3, 4})
	f.Fuzz(func(t *testing.T, typ byte, metadata uint16, data []byte) {
		if len(data) > 65536 {
			return
		}
		_, _ = (Cell{Present: true, Raw: data}).Decode(Column{Type: typ, Metadata: metadata})
	})
}
