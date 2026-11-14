// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

var recoveryDisconnect = errors.New("validation: injected connection loss")

// recoveryFault sits at a real, uncompressed protocol boundary. It is armed
// only after authentication/setup. The lost-response case first reads the
// server's complete OK, proving execution before it withholds that acknowledgement.
// Test observers use a separate unmodified connection to inspect durable effects.
type recoveryFault struct {
	armed         atomic.Bool
	dials         atomic.Int32
	mu            sync.Mutex
	command       byte
	query, phase  string
	hit, serverOK bool
	bytesSent     int
}

type recoveryConn struct {
	net.Conn
	fault        *recoveryFault
	dropResponse bool
}

func (f *recoveryFault) dial(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	f.dials.Add(1)
	return &recoveryConn{Conn: c, fault: f}, nil
}

func (c *recoveryConn) Write(p []byte) (int, error) {
	f := c.fault
	match := len(p) >= 5 && p[4] == f.command &&
		(f.query == "" || strings.EqualFold(strings.TrimSpace(string(p[5:])), f.query))
	if !match || !f.armed.CompareAndSwap(true, false) {
		return c.Conn.Write(p)
	}
	// These injected commands fit one physical packet. Refuse to pretend that
	// an arbitrary partial framing observation establishes a command boundary.
	size := int(p[0]) | int(p[1])<<8 | int(p[2])<<16
	if size != len(p)-4 {
		return 0, errors.New("validation: fault requires complete command packet")
	}
	f.mu.Lock()
	f.hit = true
	f.mu.Unlock()
	switch f.phase {
	case "before write":
		c.Conn.Close()
		return 0, recoveryDisconnect
	case "partial write":
		n, err := c.Conn.Write(p[:len(p)/2])
		c.Conn.Close()
		f.mu.Lock()
		f.bytesSent = n
		f.mu.Unlock()
		if err != nil {
			return n, err
		}
		return n, recoveryDisconnect
	case "lost OK":
		n, err := c.Conn.Write(p)
		if err == nil && n == len(p) {
			c.dropResponse = true
		}
		f.mu.Lock()
		f.bytesSent = n
		f.mu.Unlock()
		return n, err
	default:
		return 0, errors.New("validation: unknown fault phase")
	}
}

func (c *recoveryConn) Read(p []byte) (int, error) {
	if !c.dropResponse {
		return c.Conn.Read(p)
	}
	c.dropResponse = false
	defer c.Conn.Close()
	var header [4]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size < 7 || size > 1<<20 {
		return 0, errors.New("validation: invalid injected OK size")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return 0, err
	}
	if body[0] != 0 || header[3] != 1 {
		return 0, errors.New("validation: expected complete server OK")
	}
	c.fault.mu.Lock()
	c.fault.serverOK = true
	c.fault.mu.Unlock()
	return 0, recoveryDisconnect
}

func assertRecoveryError(t *testing.T, err error, number uint16, state string) {
	t.Helper()
	var server *mariadb.MySQLError
	if !errors.As(err, &server) || server.Number != number || string(server.SQLState[:]) != state || errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("expected server %d/%s without replay marker: %v", number, state, err)
	}
}

func recoveryValue(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var value int
	if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil || value != want {
		t.Fatalf("observer: got=%d want=%d err=%v", value, want, err)
	}
}

func TestMariaDBFailureRecovery(t *testing.T) {
	observer := openDB(t, "text")
	mustExec(t, observer, "DROP TABLE IF EXISTS go_stage7_recovery")
	mustExec(t, observer, "CREATE TABLE go_stage7_recovery(id INT PRIMARY KEY,v INT) ENGINE=InnoDB")
	t.Cleanup(func() { observer.ExecContext(context.Background(), "DROP TABLE go_stage7_recovery") })
	reset := func(t *testing.T) {
		mustExec(t, observer, "DELETE FROM go_stage7_recovery")
		mustExec(t, observer, "INSERT INTO go_stage7_recovery VALUES(1,10),(2,20)")
	}

	t.Run("lock timeout requires explicit rollback", func(t *testing.T) {
		reset(t)
		a, err := observer.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		b, err := observer.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		mustExec(t, b, "SET SESSION innodb_lock_wait_timeout=1")
		defer b.ExecContext(context.Background(), "SET SESSION innodb_lock_wait_timeout=50")
		var id, after uint64
		if err := b.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ta, err := a.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ta.Rollback()
		tb, err := b.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tb.Rollback()
		mustExec(t, ta, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=1")
		mustExec(t, tb, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=2")
		_, err = tb.ExecContext(t.Context(), "UPDATE go_stage7_recovery SET v=v+1 WHERE id=1")
		assertRecoveryError(t, err, 1205, "HY000")
		if err := tb.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := ta.Rollback(); err != nil {
			t.Fatal(err)
		}
		recoveryValue(t, observer, "SELECT SUM(v) FROM go_stage7_recovery", 30)
		if err := b.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&after); err != nil || after != id {
			t.Fatal("SQL error retired healthy connection", id, after, err)
		}
	})
	t.Run("deadlock victim and survivor", func(t *testing.T) {
		reset(t)
		a, err := observer.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Rollback()
		b, err := observer.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Rollback()
		mustExec(t, a, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=1")
		mustExec(t, b, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=2")
		// Both transactions already hold one lock. Launching their opposite
		// updates creates a cycle regardless of scheduler order, without sleeps.
		type outcome struct {
			tx  *sql.Tx
			err error
		}
		results := make(chan outcome, 2)
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		go func() {
			_, err := a.ExecContext(ctx, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=2")
			results <- outcome{a, err}
		}()
		go func() {
			_, err := b.ExecContext(ctx, "UPDATE go_stage7_recovery SET v=v+1 WHERE id=1")
			results <- outcome{b, err}
		}()
		victims, survivors := 0, 0
		for range 2 {
			result := <-results
			if result.err != nil {
				assertRecoveryError(t, result.err, 1213, "40001")
				victims++
				result.tx.Rollback()
			} else {
				if err := result.tx.Commit(); err != nil {
					t.Fatal(err)
				}
				survivors++
			}
		}
		if victims != 1 || survivors != 1 {
			t.Fatal("unexpected deadlock resolution", victims, survivors)
		}
		recoveryValue(t, observer, "SELECT SUM(v) FROM go_stage7_recovery", 32)
	})
	for _, operation := range []string{"autocommit", "transaction commit", "native bulk", "native stream"} {
		for _, phase := range []string{"before write", "partial write", "lost OK"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				reset(t)
				const update = "UPDATE go_stage7_recovery SET v=v+1 WHERE id=1"
				fault := &recoveryFault{phase: phase, command: 3, query: update}
				cfg := driverConfig(t, "text")
				cfg.DialFunc = fault.dial
				if operation == "native bulk" {
					fault.command = 0xfa
					fault.query = ""
					if err := cfg.Apply(mariadb.WithBulkOperations(true)); err != nil {
						t.Fatal(err)
					}
				}
				if operation == "native stream" {
					fault.command = 0x17
					fault.query = ""
				}
				db := openConfig(t, cfg)
				db.SetMaxOpenConns(1)
				db.SetMaxIdleConns(1)
				var err error
				switch operation {
				case "transaction commit":
					tx, beginErr := db.BeginTx(t.Context(), nil)
					if beginErr != nil {
						t.Fatal(beginErr)
					}
					defer tx.Rollback()
					mustExec(t, tx, update)
					fault.query = "COMMIT"
					fault.armed.Store(true)
					err = tx.Commit()
					if again := tx.Commit(); !errors.Is(again, sql.ErrTxDone) {
						t.Fatal("uncertain transaction committed twice", again)
					}
				case "autocommit":
					fault.armed.Store(true)
					_, err = db.ExecContext(t.Context(), update)
				default:
					conn, connErr := db.Conn(t.Context())
					if connErr != nil {
						t.Fatal(connErr)
					}
					fault.armed.Store(true)
					if operation == "native bulk" {
						_, err = mariadb.BulkExec(t.Context(), conn, "UPDATE go_stage7_recovery SET v=v+? WHERE id=1", [][]any{{int64(1)}})
					} else {
						_, err = mariadb.ExecStream(t.Context(), conn, "UPDATE go_stage7_recovery SET v=v+LENGTH(?) WHERE id=1", mariadb.StreamParam{Reader: bytes.NewReader([]byte("x")), MaxBytes: 1})
					}
					conn.Close()
				}
				// Only an ordinary autocommit whose first write sent zero bytes
				// is safely replayable. Native operations deliberately never retry.
				safeRetry := operation == "autocommit" && phase == "before write"
				if safeRetry {
					if err != nil {
						t.Fatal("safe pre-write retry failed", err)
					}
				} else if err == nil || (phase != "before write" && errors.Is(err, driver.ErrBadConn)) {
					t.Fatal("lost/uncertain write was successful or replayable", err)
				}
				wantDials := int32(1)
				if safeRetry {
					wantDials = 2
				}
				if fault.dials.Load() != wantDials {
					t.Fatal("unexpected automatic replay", fault.dials.Load(), wantDials)
				}
				fault.mu.Lock()
				hit, serverOK, sent := fault.hit, fault.serverOK, fault.bytesSent
				fault.mu.Unlock()
				if !hit || (phase == "lost OK" && !serverOK) || (phase == "partial write" && sent == 0) {
					t.Fatal("fault did not reach requested boundary", hit, serverOK, sent)
				}
				want := 10
				if phase == "lost OK" || safeRetry {
					want++
				}
				recoveryValue(t, observer, "SELECT v FROM go_stage7_recovery WHERE id=1", want)
				if err := db.PingContext(t.Context()); err != nil {
					t.Fatal("pool did not recover", err)
				}
				if fault.dials.Load() != 2 {
					t.Fatal("failed transport was returned to pool", fault.dials.Load())
				}
				t.Logf("RECOVERY_FAULT operation=%s phase=%s bytes=%d server_ok=%t effects=%d", operation, phase, sent, serverOK, want-10)
			})
		}
	}
}

func TestRecoveryFaultBoundary(t *testing.T) {
	// Exercise the framing guard without a real database so accidental broad
	// Write matching cannot make a live negative control look trustworthy.
	client, server := net.Pipe()
	defer server.Close()
	defer client.Close()
	fault := &recoveryFault{command: 3, phase: "before write"}
	fault.armed.Store(true)
	c := &recoveryConn{Conn: client, fault: fault}
	if _, err := c.Write([]byte{9, 0, 0, 0, 3}); err == nil || !strings.Contains(fmt.Sprint(err), "complete command packet") {
		t.Fatal(err)
	}
}
