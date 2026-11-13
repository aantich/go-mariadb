// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestCDCGTIDListPadding(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  []byte
		valid bool
		count int
	}{
		{"compact empty", make([]byte, 4), true, 0},
		{"padded empty", make([]byte, 6), true, 0},
		{"one padding byte", make([]byte, 5), false, 0},
		{"three padding bytes", make([]byte, 7), false, 0},
		{"nonzero padding", []byte{0, 0, 0, 0, 0, 1}, false, 0},
		{"truncated header", make([]byte, 3), false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := testDecoder(t)
			event, err := d.decode(wireAfter(d, GTIDListEvent, test.body, true), false)
			if test.valid {
				if err != nil || event.Checkpoint != nil || len(event.Data.(GTIDList).Entries) != test.count {
					t.Fatal(event, err)
				}
			} else if !errors.Is(err, ErrMalformed) || event != nil {
				t.Fatal("invalid GTID-list accepted", event, err)
			}
			if d.sequence != 0 || d.completed.String() != "" {
				t.Fatal("summary advanced completed progress")
			}
		})
	}
	for _, padding := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonempty", true: "nonempty with padding"}[padding], func(t *testing.T) {
			d := testDecoder(t)
			body := make([]byte, 20)
			body[0] = 1
			body[4] = 1
			body[8] = 17
			binary.LittleEndian.PutUint64(body[12:], 1)
			if padding {
				body = append(body, 0, 0)
			}
			event, err := d.decode(wireAfter(d, GTIDListEvent, body, true), false)
			if padding {
				if !errors.Is(err, ErrMalformed) || event != nil {
					t.Fatal("nonempty padding accepted", event, err)
				}
			} else if err != nil || len(event.Data.(GTIDList).Entries) != 1 {
				t.Fatal(event, err)
			}
		})
	}
}
