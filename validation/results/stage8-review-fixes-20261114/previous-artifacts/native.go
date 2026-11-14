// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
)

// withNativeConn keeps all raw driver access inside Raw's lifetime. Native
// operations own their cancellation boundary and never expose a replay marker:
// a reader may be consumed or a write may have completed before an error arrives.
func withNativeConn(ctx context.Context, conn *sql.Conn, operation func(*mysqlConn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if conn == nil {
		return errors.New("mariadb: nil SQL connection")
	}
	return conn.Raw(func(raw any) (err error) {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return errors.New("mariadb: native operation requires this driver's connection")
		}
		if err := mc.error(); err != nil {
			return err
		}
		if mc.activeRows != nil || mc.buf.busy() {
			return errors.New("mariadb: native operation requires a command boundary")
		}
		if err := mc.watchCancel(ctx); err != nil {
			return err
		}
		defer mc.finish()
		defer func() {
			if value := recover(); value != nil {
				mc.close()
				panic(value)
			}
			if canceled := mc.canceled.Value(); canceled != nil {
				err = canceled
			} else if ctx.Err() != nil {
				mc.close()
				err = ctx.Err()
			} else if errors.Is(err, driver.ErrBadConn) {
				mc.close()
				err = ErrInvalidConn
			}
		}()
		return operation(mc)
	})
}

func closeNativeStatement(stmt *mysqlStmt, err *error) {
	// This function is deferred directly by native operations. A caller Valuer
	// panic must retire before any cleanup write could wait on the transport.
	if value := recover(); value != nil {
		if stmt.mc != nil {
			stmt.mc.close()
		}
		panic(value)
	}
	if closeErr := stmt.Close(); *err == nil {
		*err = closeErr
	}
}

func convertNativeArgs(args []any) ([]driver.Value, error) {
	if len(args) > 65535 {
		return nil, errors.New("mariadb: too many prepared parameters")
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		value, err := (converter{}).ConvertValue(arg)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}
