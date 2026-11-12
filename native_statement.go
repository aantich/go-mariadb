// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"errors"
)

// ErrNativeStmtClosed is returned when a native statement is closed, nil or
// uninitialized. Cancellation before an operation takes precedence over it.
var ErrNativeStmtClosed = errors.New("mariadb: native statement is closed")

// NativeStmt reuses one server prepared statement on a caller-owned sql.Conn.
// PrepareNative creates it. Close it when finished, preferably before closing
// conn. It supports bulk DML and streamed execution, not queries returning Rows.
// It does not own a transaction or the supplied connection/readers.
//
// Methods may be called concurrently; operations on this handle are serialized.
// A canceled wait does not cancel the operation already using the handle. Do not
// copy NativeStmt or reenter its methods from a Valuer or reader. Execution is
// never automatically replayed. Managed reset reparses lazily, checking arity;
// ordinary server-side DDL reprepare retains the driver's existing semantics.
type NativeStmt struct {
	conn     *sql.Conn
	prepared *sql.Stmt
	key      *nativeStatementKey
	gate     chan struct{}
	closed   bool // Guarded by gate; no raw driver pointer is retained here.
}

// This opaque, nonzero-sized key associates a sql.Stmt with its driver statement
// inside the owning connection. database/sql owns the real statement's cleanup,
// even if the caller returns conn to the pool before closing NativeStmt.
type nativeStatementKey struct{ registered bool }
type nativePrepareContextKey struct{}

// PrepareNative prepares query once for repeated NativeStmt executions. Use a
// pinned connection obtained with db.Conn. Bulk methods require their existing
// negotiated options; streaming needs no extra capability. Close active Rows
// before preparing or using this handle. The preparation context is not retained.
func PrepareNative(ctx context.Context, conn *sql.Conn, query string) (*NativeStmt, error) {
	if err := withNativeConn(ctx, conn, func(*mysqlConn) error { return nil }); err != nil {
		return nil, err
	}
	key := &nativeStatementKey{}
	prepared, err := conn.PrepareContext(context.WithValue(ctx, nativePrepareContextKey{}, key), query)
	if err != nil {
		return nil, err
	}
	if !key.registered {
		prepared.Close()
		return nil, errors.New("mariadb: native preparation requires this driver's connection")
	}
	return &NativeStmt{conn: conn, prepared: prepared, key: key, gate: make(chan struct{}, 1)}, nil
}

func (s *NativeStmt) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.gate == nil {
		return ErrNativeStmtClosed
	}
	select {
	case s.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.gate
			return err
		}
		if s.closed {
			<-s.gate
			return ErrNativeStmtClosed
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *NativeStmt) run(ctx context.Context, required extendedCapabilityFlag, operation func(*mysqlStmt) error) error {
	// The caller holds gate across Raw. Raw serializes this operation with
	// ordinary SQL and reset. Lookup stays inside Raw; registration and cleanup
	// run in database/sql's serialized driver callbacks under the same lock.
	return withNativeConn(ctx, s.conn, func(mc *mysqlConn) error {
		if mc.extCapabilities&required != required {
			return ErrUnsupportedCapability
		}
		stmt := mc.nativeStatements[s.key]
		if stmt == nil {
			return ErrNativeStmtClosed
		}
		if err := stmt.ensurePrepared(); err != nil {
			return err
		}
		return operation(stmt)
	})
}

// BulkExec has BulkExec's bounds, conversion, transaction and unsigned-result
// contracts, while retaining the prepared handle between batches. Empty input
// sends nothing. It never implicitly splits a batch or retries a write.
func (s *NativeStmt) BulkExec(ctx context.Context, rows [][]any) (UnsignedResult, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-s.gate }()
	if len(rows) == 0 {
		return &mysqlResult{affectedRows: []int64{0}, insertIds: []int64{0}}, nil
	}
	var result UnsignedResult
	err := s.run(ctx, clientStmtBulkOperations, func(stmt *mysqlStmt) (err error) {
		result, err = stmt.execBulk(ctx, rows)
		return err
	})
	return result, err
}

// BulkExecUnits has BulkExecUnits's owned uint64 unit-result contract and reuses
// this handle. A server error returns no purportedly complete unit list. Use an
// explicit transaction when application atomicity is required.
func (s *NativeStmt) BulkExecUnits(ctx context.Context, rows [][]any) ([]BulkUnitResult, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-s.gate }()
	if len(rows) == 0 {
		return []BulkUnitResult{}, nil
	}
	var result []BulkUnitResult
	err := s.run(ctx, clientStmtBulkOperations|clientUnitBulkResult, func(stmt *mysqlStmt) (err error) {
		result, err = stmt.execBulkUnits(ctx, rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ExecStream has ExecStream's reader ownership, validation and cleanup contract,
// without preparing and closing for each execution. Supply fresh readers on
// every call. An ordinary argument may change type, signedness or NULL status
// between calls; the driver sends the complete parameter types each time.
func (s *NativeStmt) ExecStream(ctx context.Context, args ...any) (UnsignedResult, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-s.gate }()
	var result UnsignedResult
	err := s.run(ctx, 0, func(stmt *mysqlStmt) error {
		values, err := streamArgs(ctx, stmt.mc, args)
		if err != nil {
			return err
		}
		out, err := stmt.Exec(values)
		if err == nil {
			result = out.(UnsignedResult)
		}
		return err
	})
	return result, err
}

// Close releases the prepared statement but leaves conn open. It is idempotent,
// waits for operations on this handle, and uses database/sql's normal statement
// cleanup, including after conn.Close. Like sql.Stmt.Close it has no context;
// configure WriteTimeout to bound a stalled cleanup write. Cancel active work
// with its execution context before waiting for Close.
// If another caller has active Rows on the physical connection, server cleanup
// waits for their Close; a cleanup failure is then reported by Rows.Close/Err.
func (s *NativeStmt) Close() error {
	if s == nil || s.gate == nil {
		return nil
	}
	s.gate <- struct{}{}
	defer func() { <-s.gate }()
	if s.closed {
		return nil
	}
	s.closed = true
	prepared := s.prepared
	s.prepared, s.conn, s.key = nil, nil, nil
	return prepared.Close()
}
