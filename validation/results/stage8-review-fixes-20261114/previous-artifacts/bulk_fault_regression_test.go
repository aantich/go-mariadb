// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type bulkFaultConnector struct{ connection *mysqlConn }

func (c bulkFaultConnector) Connect(context.Context) (driver.Conn, error) { return c.connection, nil }
func (bulkFaultConnector) Driver() driver.Driver                          { return &MariaDBDriver{} }

func readFaultCommand(conn net.Conn) error {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return err
	}
	_, err := io.CopyN(io.Discard, conn, int64(getUint24(header[:3])))
	return err
}

func TestDedicatedBulkCancellationPhases(t *testing.T) {
	for _, phase := range []string{"prepare_write", "prepare_reply", "execute_write", "execute_reply", "close_write"} {
		t.Run(phase, func(t *testing.T) {
			client, peer := net.Pipe()
			mc := &mysqlConn{buf: newBuffer(), cfg: NewConfig(), netConn: client, rawConn: client, closech: make(chan struct{}),
				maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF, extCapabilities: clientStmtBulkOperations}
			mc.startWatcher()
			db := sql.OpenDB(bulkFaultConnector{mc})
			defer db.Close()
			defer peer.Close()
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if phase == "prepare_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "prepare_reply" {
					return
				}
				// Prepare one parameter, no result columns; parameter definitions
				// are discarded but still count against the metadata budget.
				response := makePacket(1, []byte{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0})
				response = append(response, makePacket(2, []byte{0})...)
				if _, err := peer.Write(response); err != nil || phase == "execute_write" {
					return
				}
				if readFaultCommand(peer) != nil || phase == "execute_reply" {
					return
				}
				peer.Write(makePacket(1, []byte{iOK, 1, 0, 2, 0, 0, 0}))
				// Do not read COM_STMT_CLOSE. It must remain cancellable too.
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			_, err = BulkExec(ctx, conn, "INSERT INTO t VALUES (?)", [][]any{{int64(1)}})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("phase=%s error=%v", phase, err)
			}
			if !mc.closed.Load() {
				t.Error("canceled connection remained usable")
			}
			peer.Close()
			<-finished
		})
	}
}
