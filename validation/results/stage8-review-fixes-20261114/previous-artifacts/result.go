// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2012 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mariadb

import (
	"database/sql/driver"
	"errors"
	"slices"
)

// Result exposes multi-statement counters not available through sql.Result.
//
// This is accessible by executing statements using sql.Conn.Raw() and
// downcasting the returned result:
//
//	res, err := rawConn.Exec(...)
//	res.(mariadb.Result).AllRowsAffected()
type Result interface {
	driver.Result
	// AllRowsAffected returns a slice containing the affected rows for each
	// executed statement. Values above MaxInt64 have their original signed bit
	// pattern; use UnsignedResult for lossless numeric interpretation.
	AllRowsAffected() []int64
	// AllLastInsertIds returns a slice containing the last inserted ID for each
	// executed statement. Values above MaxInt64 retain their signed bit pattern;
	// use UnsignedResult for lossless numeric interpretation.
	AllLastInsertIds() []int64
}

type mysqlResult struct {
	// One entry in both slices is created for every executed statement result.
	affectedRows []int64
	insertIds    []int64
}

// ErrResultOverflow means a server's unsigned counter cannot be represented by
// database/sql's signed result API. Use UnsignedResult to retrieve its full value.
var ErrResultOverflow = errors.New("mariadb: unsigned result exceeds int64")

// UnsignedResult preserves MariaDB's complete unsigned OK counters. Results
// returned by ExecUnsigned and BulkExec implement this interface. database/sql
// wraps ordinary Exec results and does not expose extension interfaces. The
// slices are defensive copies, independent of later commands and pool reuse.
type UnsignedResult interface {
	driver.Result
	LastInsertIDUnsigned() uint64
	RowsAffectedUnsigned() uint64
	AllLastInsertIDsUnsigned() []uint64
	AllRowsAffectedUnsigned() []uint64
}

func (res *mysqlResult) LastInsertIDUnsigned() uint64 {
	return uint64(res.insertIds[len(res.insertIds)-1])
}
func (res *mysqlResult) RowsAffectedUnsigned() uint64 {
	return uint64(res.affectedRows[len(res.affectedRows)-1])
}
func unsignedResults(values []int64) []uint64 {
	if values == nil {
		return nil
	}
	out := make([]uint64, len(values))
	for i, n := range values {
		out[i] = uint64(n)
	}
	return out
}
func (res *mysqlResult) AllLastInsertIDsUnsigned() []uint64 { return unsignedResults(res.insertIds) }
func (res *mysqlResult) AllRowsAffectedUnsigned() []uint64  { return unsignedResults(res.affectedRows) }

func (res *mysqlResult) LastInsertId() (int64, error) {
	if res.insertIds[len(res.insertIds)-1] < 0 {
		return 0, ErrResultOverflow
	}
	return res.insertIds[len(res.insertIds)-1], nil
}

func (res *mysqlResult) RowsAffected() (int64, error) {
	if res.affectedRows[len(res.affectedRows)-1] < 0 {
		return 0, ErrResultOverflow
	}
	return res.affectedRows[len(res.affectedRows)-1], nil
}

func (res *mysqlResult) AllLastInsertIds() []int64 {
	return slices.Clone(res.insertIds) // defensive copy
}

func (res *mysqlResult) AllRowsAffected() []int64 {
	return slices.Clone(res.affectedRows) // defensive copy
}
