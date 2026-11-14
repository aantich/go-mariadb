// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

//go:build go1.27

package mariadb

import "database/sql/driver"

var _ driver.RowsColumnScanner = (*cursorRows)(nil)

func (rows *cursorRows) NextRow() error { return rows.next(rows.binaryRows.NextRow) }
