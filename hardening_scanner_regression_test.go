// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

//go:build go1.27

package mariadb

import (
	"database/sql/driver"
	"errors"
	"testing"
)

func TestDedicatedMalformedDirectRowsRetire(t *testing.T) {
	for _, binary := range []bool{false, true} {
		for _, payload := range [][]byte{{0xfc}, {1, 'x', 0}} {
			if binary {
				payload = append([]byte{0, 0}, payload...)
			}
			rows, _, mc := scannerTestRows(binary, []mysqlField{{fieldType: fieldTypeString}}, payload, false)
			if err := rows.NextRow(); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
				t.Fatalf("binary=%v err=%v retired=%v", binary, err, mc.closed.Load())
			}
		}
	}
}

func TestDedicatedDirectConversionErrorKeepsConnection(t *testing.T) {
	rows, _, mc := scannerTestRows(false, []mysqlField{{fieldType: fieldTypeString}}, []byte{3, 'a', 'b', 'c'}, false)
	if err := rows.NextRow(); err != nil {
		t.Fatal(err)
	}
	if err := rows.ScanColumn(driver.ScanContext{}, 0, new(int)); err == nil {
		t.Fatal("incompatible destination accepted")
	}
	if mc.closed.Load() {
		t.Fatal("application conversion error retired a valid wire stream")
	}
}
