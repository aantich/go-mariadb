// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"testing"

	mariadb "github.com/aantich/go-mariadb"
)

func TestMariaDBCursorMaterializationError(t *testing.T) {
	for _, mode := range nativeModes {
		t.Run(mode, func(t *testing.T) {
			conn := pinned(t, openConfig(t, stage3Config(t, mode, mariadb.WithServerCursors(2))))
			const query = "SELECT t.seq,(SELECT q.seq FROM seq_1_to_2 q WHERE q.seq<=t.seq) AS v FROM seq_1_to_3 t"
			// The scalar subquery succeeds on the first row, then fails while
			// MariaDB materializes the cursor, after sending its column metadata.
			stmt, err := conn.PrepareContext(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			for range 2 { // Repeat on the same handle, including cached metadata.
				rows, err := stmt.QueryContext(t.Context())
				if rows != nil {
					rows.Close()
					t.Fatal("failed cursor unexpectedly returned rows")
				}
				var server *mariadb.MySQLError
				if !errors.As(err, &server) || server.Number != 1242 || string(server.SQLState[:]) != "21000" {
					t.Fatalf("lost cursor SQL error: %v", err)
				}
				if err := conn.PingContext(t.Context()); err != nil {
					t.Fatalf("terminal SQL error retired connection: %v", err)
				}
			}
		})
	}
}

func TestMariaDBStreamPacketBoundary(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			cfg := stage3Config(t, "text")
			cfg.MaxAllowedPacket = 65536
			if automatic {
				cfg.MaxAllowedPacket = 0
			}
			conn := pinned(t, openConfig(t, cfg))
			mustExec(t, conn, "CREATE TEMPORARY TABLE go_stage3_packet(b LONGBLOB)")
			for _, size := range []int{65528, 65529, 65530} {
				payload := bytes.Repeat([]byte{'x'}, size)
				for _, stream := range []bool{false, true} {
					mustExec(t, conn, "DELETE FROM go_stage3_packet")
					var err error
					if stream {
						_, err = mariadb.ExecStream(t.Context(), conn, "INSERT INTO go_stage3_packet VALUES (?)", mariadb.StreamParam{Reader: bytes.NewReader(payload)})
					} else {
						_, err = conn.ExecContext(t.Context(), "INSERT INTO go_stage3_packet VALUES (?)", payload)
					}
					if err != nil {
						t.Fatalf("size=%d stream=%t: %v", size, stream, err)
					}
					// A digest avoids creating a result packet near the receive limit.
					var count, length int
					var digest string
					if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*),MAX(OCTET_LENGTH(b)),MAX(MD5(b)) FROM go_stage3_packet").Scan(&count, &length, &digest); err != nil || count != 1 || length != size || digest != fmt.Sprintf("%x", md5.Sum(payload)) {
						t.Fatalf("size=%d stream=%t count=%d length=%d digest=%s error=%v", size, stream, count, length, digest, err)
					}
				}
			}
		})
	}
}
