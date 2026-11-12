// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// StreamParam supplies one one-shot BLOB/TEXT parameter to ExecStream. The
// reader remains caller-owned and is not closed. MaxBytes=0 uses MaxAllowedPacket;
// a positive limit must not exceed it. ChunkBytes=0 uses 64 KiB; at most 1 MiB
// is permitted. Chunking bounds client staging, not server storage or packet
// limits on the complete parameter. An empty stream is an empty value, not NULL.
//
// Read must return when its request is canceled: a general io.Reader cannot be
// forcibly interrupted by a context. Do not reuse a consumed reader on retry.
type StreamParam struct {
	Reader     io.Reader
	MaxBytes   int64
	ChunkBytes int
}

type streamInput struct {
	StreamParam
	ctx context.Context
}

// ExecStream executes a prepared statement on a pinned connection. Only this
// helper accepts StreamParam values; ordinary Exec/Valuer contracts are unchanged.
// All ordinary arguments and stream limits are validated before reading sources
// or sending parameter data. Reader errors reset unexecuted uploads; reset or
// transport failure retires the connection. Cancellation never revives it.
// Execution can complete before a response is lost, so no error requests replay.
// Close active rows first. conn and every reader remain caller-owned.
func ExecStream(ctx context.Context, conn *sql.Conn, query string, args ...any) (UnsignedResult, error) {
	var result UnsignedResult
	err := withNativeConn(ctx, conn, func(mc *mysqlConn) (err error) {
		if len(args) > 65535 {
			return errors.New("mariadb: too many prepared parameters")
		}
		values := make([]driver.Value, len(args))
		for i, arg := range args {
			if stream, ok := arg.(StreamParam); ok {
				if stream.Reader == nil || stream.MaxBytes < 0 || stream.MaxBytes > int64(mc.maxAllowedPacket) || stream.ChunkBytes < 0 || stream.ChunkBytes > 1<<20 {
					return fmt.Errorf("mariadb: invalid stream parameter %d", i)
				}
				if stream.MaxBytes == 0 {
					stream.MaxBytes = int64(mc.maxAllowedPacket)
				}
				if stream.ChunkBytes == 0 {
					stream.ChunkBytes = 64 << 10
				}
				// MariaDB rejects a command body whose size reaches the server
				// maximum. Reserve LONG_DATA's seven bytes and one spare byte,
				// as the ordinary buffered parameter path does.
				stream.ChunkBytes = min(stream.ChunkBytes, mc.maxAllowedPacket-1-7)
				if stream.ChunkBytes < 1 {
					return ErrPktTooLarge
				}
				values[i] = streamInput{stream, ctx}
			} else {
				value, err := (converter{}).ConvertValue(arg)
				if err != nil {
					return err
				}
				values[i] = value
			}
		}
		prepared, err := mc.Prepare(query)
		if err != nil {
			return err
		}
		stmt := prepared.(*mysqlStmt)
		defer closeNativeStatement(stmt, &err)
		out, err := stmt.Exec(values)
		if err == nil {
			result = out.(UnsignedResult)
		}
		return err
	})
	return result, err
}

func (stmt *mysqlStmt) writeStreamLongData(paramID int, input streamInput) error {
	mc := stmt.mc
	// A caller's reader panic must not leave partially uploaded data attached
	// to a pooled connection. Retire before statement cleanup unwinds.
	defer func() {
		if value := recover(); value != nil {
			mc.close()
			panic(value)
		}
	}()
	packet := make([]byte, 11+input.ChunkBytes)
	packet[4] = comStmtSendLongData
	binary.LittleEndian.PutUint32(packet[5:9], stmt.id)
	binary.LittleEndian.PutUint16(packet[9:11], uint16(paramID))
	var total int64
	emptyReads, sent := 0, false
	for {
		if err := input.ctx.Err(); err != nil {
			return err
		}
		// Probe one extra byte at the limit to distinguish EOF from overflow.
		remaining := input.MaxBytes - total
		space := min(int64(input.ChunkBytes), remaining)
		if remaining < int64(input.ChunkBytes) {
			space++ // Probe without overflowing when MaxBytes is MaxInt64.
		}
		n, readErr := input.Reader.Read(packet[11 : 11+int(space)])
		if n < 0 || n > int(space) {
			return errors.New("mariadb: stream reader returned an invalid byte count")
		}
		if err := input.ctx.Err(); err != nil {
			return err
		}
		if int64(n) > input.MaxBytes-total {
			return errors.New("mariadb: stream parameter exceeds MaxBytes")
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if n > 0 || (!sent && readErr == io.EOF) {
			if err := mc.writePacket(packet[:11+n]); err != nil {
				return err
			}
			mc.resetSequence() // LONG_DATA has no response; each chunk is a command.
			sent = true
			total += int64(n)
			emptyReads = 0
		} else {
			emptyReads++
			if emptyReads >= 100 {
				return io.ErrNoProgress
			}
		}
		if readErr == io.EOF {
			return nil
		}
	}
}
