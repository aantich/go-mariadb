// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrUnsupportedCapability means the requested native operation was not
// negotiated. Bulk execution never silently falls back to different semantics.
var ErrUnsupportedCapability = errors.New("mariadb: native capability unavailable")

// BulkIndicator supplies a MariaDB per-parameter bulk instruction. Use nil for
// SQL NULL. DEFAULT and IGNORE have server-defined meanings for the chosen DML;
// they are not strings substituted into SQL.
type BulkIndicator byte

const (
	// BulkDefault requests the column's default value.
	BulkDefault BulkIndicator = 2
	// BulkIgnore tells the server to ignore this parameter where supported.
	BulkIgnore BulkIndicator = 3
)

// WithBulkOperations opts in to MariaDB's native COM_STMT_BULK_EXECUTE capability.
// The server must advertise support. It does not enable per-unit result packets.
func WithBulkOperations(enabled bool) Option {
	return func(c *Config) error { c.bulkOperations = enabled; return nil }
}

// BulkExec executes one bounded native binary batch on a pinned SQL connection.
// Every row must have the same arity and each column a consistent concrete wire
// type (signed and unsigned integers are distinct). nil, BulkDefault and
// BulkIgnore do not determine a column's wire type. Supported values are the
// ordinary driver values, uint64 and driver.Valuer implementations.
//
// All values are converted and encoded before sending the execute command. A
// batch is limited to 65535 rows, 1<<20 cells and MaxAllowedPacket bytes; it is
// never split implicitly. Empty input returns zero counters without sending SQL.
// A server error may follow partial effects; use BEGIN/COMMIT/ROLLBACK on conn
// when application atomicity is required. Cancellation retires the connection,
// and the helper never requests automatic write replay. The supplied connection
// remains owned by the caller. Close it after use.
func BulkExec(ctx context.Context, conn *sql.Conn, query string, rows [][]any) (UnsignedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &mysqlResult{affectedRows: []int64{0}, insertIds: []int64{0}}, nil
	}
	if conn == nil {
		return nil, fmt.Errorf("mariadb: nil SQL connection")
	}
	var result UnsignedResult
	err := conn.Raw(func(raw any) (err error) {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return fmt.Errorf("mariadb: BulkExec requires this driver's connection")
		}
		if mc.extCapabilities&clientStmtBulkOperations == 0 {
			return ErrUnsupportedCapability
		}
		if mc.closed.Load() {
			return ErrInvalidConn
		}
		if err := mc.watchCancel(ctx); err != nil {
			return err
		}
		defer mc.finish()
		prepared, err := mc.Prepare(query)
		if err != nil {
			return err
		}
		stmt := prepared.(*mysqlStmt)
		defer func() {
			if closeErr := stmt.Close(); err == nil {
				err = closeErr
			}
		}()
		result, err = stmt.execBulk(ctx, rows)
		return err
	})
	return result, err
}

// execBulk shares the validated wire path; its caller owns the command boundary.
func (stmt *mysqlStmt) execBulk(ctx context.Context, rows [][]any) (UnsignedResult, error) {
	mc := stmt.mc
	packet, err := stmt.encodeBulk(rows)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mc.resetSequence()
	if err := mc.writePacket(packet); err != nil {
		return nil, err
	}
	// Compression shares scratch storage between writes and reads. Finish
	// the command exactly as ordinary execution does before reading a reply.
	mc.syncSequence()
	handler := mc.clearResult()
	columns, _, err := handler.readResultSetHeaderPacket()
	if err != nil {
		return nil, err
	}
	// Bulk SELECT is outside this DML API. Retire rather than returning a
	// connection with unread metadata/rows to database/sql's pool.
	if columns != 0 {
		return nil, mc.protocolError()
	}
	if err := handler.discardResults(); err != nil {
		return nil, err
	}
	copied := mc.result
	return &copied, nil
}

func bulkType(value driver.Value) (uint16, int, error) {
	switch v := value.(type) {
	case nil:
		return uint16(fieldTypeNULL), 0, nil
	case int64:
		return uint16(fieldTypeLongLong), 8, nil
	case uint64:
		return uint16(fieldTypeLongLong) | 0x8000, 8, nil
	case bool:
		return uint16(fieldTypeTiny), 1, nil
	case float64:
		return uint16(fieldTypeDouble), 8, nil
	case string:
		return uint16(fieldTypeString), bulkStringSize(len(v)), nil
	case []byte:
		return uint16(fieldTypeString), bulkStringSize(len(v)), nil
	}
	return 0, 0, fmt.Errorf("mariadb: unsupported bulk value %T", value)
}

// Use the actual length-prefix width: pessimistically charging nine bytes for
// every short string both wastes the batch buffer and rejects fitting packets.
func bulkStringSize(n int) int {
	switch {
	case n <= 250:
		return n + 1
	case n <= 65535:
		return n + 3
	case n <= 0xffffff:
		return n + 4
	default:
		return n + 9
	}
}

func (stmt *mysqlStmt) encodeBulk(rows [][]any) ([]byte, error) {
	n := stmt.paramCount
	if n == 0 || len(rows) > 65535 || len(rows) > (1<<20)/n {
		return nil, fmt.Errorf("mariadb: invalid or oversized bulk dimensions")
	}
	values := make([]driver.Value, len(rows)*n)
	indicators := make([]byte, len(values))
	types := make([]uint16, n)
	known := make([]bool, n)
	size, limit := 7+2*n, stmt.mc.maxAllowedPacket
	for r, row := range rows {
		if len(row) != n {
			return nil, fmt.Errorf("mariadb: bulk row %d has %d parameters; want %d", r, len(row), n)
		}
		for c, input := range row {
			i := r*n + c
			if marker, ok := input.(BulkIndicator); ok {
				if marker != BulkDefault && marker != BulkIgnore {
					return nil, fmt.Errorf("mariadb: invalid bulk indicator")
				}
				indicators[i] = byte(marker)
			} else {
				value, err := (converter{}).ConvertValue(input)
				if err != nil {
					return nil, fmt.Errorf("mariadb: bulk row %d column %d: %w", r, c, err)
				}
				if b, ok := value.([]byte); ok && b == nil {
					value = nil
				}
				if date, ok := value.(time.Time); ok {
					// Normalize once, before packet allocation, so invalid dates
					// fail validation and their actual encoded size is charged.
					if date.IsZero() {
						// Match ordinary prepared parameters before timezone conversion:
						// Go's zero instant denotes MariaDB's zero date, not year 0001.
						value = "0000-00-00"
					} else {
						value, err = appendDateTime(nil, date.In(stmt.mc.cfg.Loc), stmt.mc.cfg.timeTruncate)
					}
					if err != nil {
						return nil, err
					}
				}
				values[i] = value
				if value == nil {
					indicators[i] = 1
				} else {
					typ, bytes, err := bulkType(value)
					if err != nil {
						return nil, err
					}
					if known[c] && types[c] != typ {
						return nil, fmt.Errorf("mariadb: inconsistent bulk type at row %d column %d", r, c)
					}
					types[c], known[c] = typ, true
					if bytes > limit-size {
						return nil, ErrPktTooLarge
					}
					size += bytes
				}
			}
			if size >= limit {
				return nil, ErrPktTooLarge
			}
			size++
		}
	}
	data := make([]byte, 4, 4+size)
	data = append(data, 0xfa)
	data = binary.LittleEndian.AppendUint32(data, stmt.id)
	data = binary.LittleEndian.AppendUint16(data, 128) // CLIENT_SEND_TYPES
	for i, typ := range types {
		if !known[i] {
			typ = uint16(fieldTypeNULL)
		}
		data = binary.LittleEndian.AppendUint16(data, typ)
	}
	for i, value := range values {
		data = append(data, indicators[i])
		if indicators[i] != 0 {
			continue
		}
		switch v := value.(type) {
		case int64:
			data = binary.LittleEndian.AppendUint64(data, uint64(v))
		case uint64:
			data = binary.LittleEndian.AppendUint64(data, v)
		case float64:
			data = binary.LittleEndian.AppendUint64(data, math.Float64bits(v))
		case bool:
			if v {
				data = append(data, 1)
			} else {
				data = append(data, 0)
			}
		case string:
			data = appendLengthEncodedString(data, v)
		case []byte:
			data = appendLengthEncodedInteger(data, uint64(len(v)))
			data = append(data, v...)
		}
	}
	return data, nil
}
