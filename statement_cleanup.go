// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

// statementClose keeps only the server identity. A returned sql.Conn may already
// be serving another caller when its old statement is closed. database/sql's
// lock serializes calls, not the entire lifetime of that caller's Rows.
type statementClose struct {
	id         uint32
	generation uint64
}

func (mc *mysqlConn) trackRows(rows *mysqlRows) {
	rows.owner = mc
	mc.activeRows = rows
}

// releaseStatements runs once, after Rows has drained/reset its response and
// borrowed values have expired, but before its cancellation watcher finishes.
// Ownership survives readRowPacket clearing rows.mc at a final or batch EOF.
func (rows *mysqlRows) releaseStatements(err *error) {
	mc := rows.owner
	rows.owner = nil
	if mc == nil || mc.activeRows != rows {
		return
	}
	mc.activeRows = nil
	if closeErr := mc.flushStatementCloses(); *err == nil {
		*err = closeErr
	}
}

func (mc *mysqlConn) flushStatementCloses() error {
	pending := mc.deferredCloses
	mc.deferredCloses = nil
	if mc.closed.Load() {
		return nil // Retirement already releases every server statement.
	}
	for _, close := range pending {
		if close.generation != mc.generation {
			continue // A session reset removed this ID; it may have been recycled.
		}
		if mc.buf.busy() {
			mc.close()
			return ErrBusyBuffer // Never reset sequences on an unread response.
		}
		if err := mc.writeCommandPacketUint32(comStmtClose, close.id); err != nil {
			mc.close()
			return err
		}
	}
	return nil
}
