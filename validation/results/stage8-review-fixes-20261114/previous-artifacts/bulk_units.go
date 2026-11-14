// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"encoding/binary"
	"io"
	"strings"
)

// BulkUnitResult is one server-reported bulk unit, in wire order. Both counters
// preserve all 64 bits. Units describe MariaDB's execution, not row-error records
// or a guarantee of one result for every input row under every DML optimization.
type BulkUnitResult struct {
	InsertID     uint64
	RowsAffected uint64
}

// WithBulkUnitResults requests native bulk execution and its unit-result
// capability. BulkExec still returns aggregate results. BulkExecUnits reports
// ErrUnsupportedCapability when the server did not negotiate both flags.
func WithBulkUnitResults(enabled bool) Option {
	return func(c *Config) error { c.bulkUnitResults = enabled; return nil }
}

// BulkExecUnits executes bounded DML without RETURNING on a pinned connection.
// It returns owned units only after the complete, validated response arrives.
// Input conversion/limits match BulkExec. A server error can follow partial
// effects and returns no purportedly complete unit list; use an explicit
// transaction for atomicity. No command is automatically replayed. Close active
// rows first; conn remains owned by the caller. Empty input sends nothing.
func BulkExecUnits(ctx context.Context, conn *sql.Conn, query string, rows [][]any) ([]BulkUnitResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []BulkUnitResult{}, nil
	}
	var units []BulkUnitResult
	err := withNativeConn(ctx, conn, func(mc *mysqlConn) (err error) {
		if mc.extCapabilities&(clientStmtBulkOperations|clientUnitBulkResult) != clientStmtBulkOperations|clientUnitBulkResult {
			return ErrUnsupportedCapability
		}
		prepared, err := mc.Prepare(query)
		if err != nil {
			return err
		}
		stmt := prepared.(*mysqlStmt)
		defer closeNativeStatement(stmt, &err)
		units, err = stmt.execBulkUnits(ctx, rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	return units, nil
}

// execBulkUnits retains the same schema, bounds and complete-response contract
// for both one-shot calls and a retained prepared statement.
func (stmt *mysqlStmt) execBulkUnits(ctx context.Context, rows [][]any) ([]BulkUnitResult, error) {
	mc := stmt.mc
	packet, err := stmt.encodeBulk(rows)
	if err != nil {
		return nil, err
	}
	packet[9] |= 64 // STMT_BULK_FLAG_SEND_UNIT_RESULTS; existing flag is 128.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mc.resetSequence()
	if err := mc.writePacket(packet); err != nil {
		return nil, err
	}
	mc.syncSequence()
	n, follows, err := mc.clearResult().readResultSetHeaderPacket()
	if err != nil {
		return nil, err
	}
	if n != 2 || !follows {
		return nil, mc.protocolError()
	}
	columns, err := mc.readColumns(n, nil)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(columns[0].name, "Id") || !strings.EqualFold(columns[1].name, "Affected_rows") ||
		columns[0].fieldType != fieldTypeLongLong || columns[1].fieldType != fieldTypeLongLong {
		return nil, mc.protocolError()
	}
	resultRows := &binaryRows{mysqlRows{mc: mc, rs: resultSet{columns: columns}}}
	units := make([]BulkUnitResult, 0, len(rows))
	for {
		data, readErr := resultRows.readRowPacket(true)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		// Unit counters are unsigned even on peers whose metadata declares
		// BIGINT without UNSIGNED. Do not pass them through signed scanning.
		if len(data) != 18 || data[1] != 0 || len(units) >= len(rows) {
			return nil, mc.protocolError()
		}
		units = append(units, BulkUnitResult{binary.LittleEndian.Uint64(data[2:10]), binary.LittleEndian.Uint64(data[10:18])})
	}
	if mc.status&statusMoreResultsExists != 0 {
		return nil, mc.protocolError() // RETURNING/multiple result sets are not units.
	}
	return units, nil
}
