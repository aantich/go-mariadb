// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
)

// ExecUnsigned executes SQL on a caller-owned pinned connection and exposes
// MariaDB's unsigned counters. database/sql wraps driver results, so an ordinary
// sql.Result cannot be asserted to UnsignedResult. Use this helper when IDs may
// exceed MaxInt64. Parameters use a transient prepared statement; caller-owned
// transaction boundaries remain unchanged. The connection is not closed here.
func ExecUnsigned(ctx context.Context, conn *sql.Conn, query string, args ...any) (UnsignedResult, error) {
	if conn == nil {
		return nil, fmt.Errorf("mariadb: nil SQL connection")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		value, err := (converter{}).ConvertValue(arg)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	var result UnsignedResult
	err := conn.Raw(func(raw any) (err error) {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return fmt.Errorf("mariadb: ExecUnsigned requires this driver's connection")
		}
		if err := mc.watchCancel(ctx); err != nil {
			return err
		}
		defer mc.finish()
		var res driver.Result
		if len(values) == 0 {
			res, err = mc.Exec(query, nil)
		} else {
			var prepared driver.Stmt
			prepared, err = mc.Prepare(query)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := prepared.Close(); err == nil {
					err = closeErr
				}
			}()
			res, err = prepared.Exec(values)
		}
		if err == nil {
			result = res.(UnsignedResult)
		}
		return err
	})
	return result, err
}
