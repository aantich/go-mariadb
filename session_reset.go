// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SessionResetPolicy requests clean pool reuse. Config.DBName must name the
// database to restore: MariaDB's RESET command does not clear the current schema.
// An empty Role means NONE; otherwise Role is a literal role name, not SQL.
// InitSQL contains trusted initialization statements run in order on both fresh
// connections and resets, after charset, role, database and Config.Params.
// Initialization must leave no transaction open. Options are copied by Apply.
//
// Managed checkout adds protocol round trips and invalidates server prepared
// statements. Ordinary connections retain their existing liveness-only policy.
type SessionResetPolicy struct {
	Role    string
	InitSQL []string
}

// WithSessionReset enables reset/restoration on pool checkout. Use NewConnector
// and sql.OpenDB: functional options are not serialized by Config.FormatDSN.
func WithSessionReset(policy SessionResetPolicy) Option {
	return func(c *Config) error {
		if strings.IndexByte(policy.Role, 0) >= 0 {
			return errors.New("mariadb: session role contains NUL")
		}
		copy := policy
		copy.InitSQL = slices.Clone(policy.InitSQL)
		c.sessionReset = &copy
		return nil
	}
}

func (cfg *Config) validateSessionPolicy() error {
	if cfg.sessionReset != nil && (cfg.DBName == "" || strings.IndexByte(cfg.DBName, 0) >= 0) {
		return errors.New("mariadb: managed session reset requires a nonempty database name without NUL")
	}
	return nil
}

// ResetSession resets a pinned connection using its configured managed policy.
// It rolls back unfinished work, removes temporary tables/user variables and
// invalidates prepared IDs. Existing Go statement handles reprepare lazily.
// Call only after closing all rows/transactions on conn. On any reset or
// restoration failure the physical connection is retired, without write replay.
// The caller still owns conn. A connection without WithSessionReset is rejected.
func ResetSession(ctx context.Context, conn *sql.Conn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if conn == nil {
		return errors.New("mariadb: nil SQL connection")
	}
	return conn.Raw(func(raw any) error {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return errors.New("mariadb: ResetSession requires this driver's connection")
		}
		if mc.cfg.sessionReset == nil {
			return errors.New("mariadb: managed session reset is not enabled")
		}
		return mc.resetSession(ctx)
	})
}

func (mc *mysqlConn) resetSession(ctx context.Context) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = mc.error(); err != nil {
		return err
	}
	if mc.activeRows != nil || mc.buf.busy() {
		return errors.New("mariadb: reset requires a command boundary")
	}
	if err = mc.watchCancel(ctx); err != nil {
		return err
	}
	defer mc.finish()
	defer func() {
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			mc.close()
			if canceled := mc.canceled.Value(); canceled != nil {
				err = canceled
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
		}
	}()
	// Invalidate before sending: a lost RESET reply cannot make an old ID safe.
	mc.generation++
	mc.session = nil
	if err = mc.writeCommandPacket(comResetConnection); err != nil {
		return err
	}
	if err = mc.clearResult().readResultOK(); err != nil {
		return err
	}
	return mc.restoreManagedSession()
}

func (mc *mysqlConn) restoreCharset() error {
	var last error
	for _, charset := range mc.cfg.charsets {
		query := "SET NAMES " + charset
		if mc.cfg.Collation != "" {
			query += " COLLATE " + mc.cfg.Collation
		}
		last = mc.exec(query)
		if last == nil {
			return nil
		}
		if mc.closed.Load() {
			return mc.error()
		}
	}
	return last
}

func (mc *mysqlConn) restoreManagedSession() error {
	if len(mc.cfg.charsets) != 0 {
		if err := mc.restoreCharset(); err != nil {
			return err
		}
	} else {
		collation := mc.cfg.Collation
		if collation == "" {
			collation = "utf8mb4_general_ci"
		}
		charset, _, _ := strings.Cut(collation, "_")
		if err := mc.exec("SET NAMES " + charset + " COLLATE " + collation); err != nil {
			return err
		}
	}
	policy := mc.cfg.sessionReset
	role := "NONE"
	if policy.Role != "" {
		role = "`" + strings.ReplaceAll(policy.Role, "`", "``") + "`"
	}
	if err := mc.exec("SET ROLE " + role); err != nil {
		return err
	}
	if err := mc.writeCommandPacketStr(comInitDB, mc.cfg.DBName); err != nil {
		return err
	}
	if err := mc.clearResult().readResultOK(); err != nil {
		return err
	}
	if err := mc.handleParams(); err != nil {
		return err
	}
	for i, query := range policy.InitSQL {
		if err := mc.exec(query); err != nil {
			return fmt.Errorf("mariadb: session initialization statement %d: %w", i+1, err)
		}
	}
	if mc.status&statusInTrans != 0 {
		return errors.New("mariadb: session initialization left a transaction open")
	}
	return nil
}

// ensurePrepared runs only at an execution boundary, before any caller writes.
// A changed arity is an error rather than an invitation to replay old arguments.
func (stmt *mysqlStmt) ensurePrepared() error {
	if stmt.mc == nil {
		return ErrInvalidConn
	}
	if stmt.session == nil || stmt.session.generation == stmt.mc.generation {
		return nil
	}
	prepared, err := stmt.mc.Prepare(stmt.session.queryText)
	if err != nil {
		return err
	}
	replacement := prepared.(*mysqlStmt)
	if replacement.paramCount != stmt.paramCount {
		if err := replacement.Close(); err != nil {
			stmt.mc.close()
			return err
		}
		return errors.New("mariadb: prepared parameter count changed after session reset")
	}
	stmt.id, stmt.columns = replacement.id, replacement.columns
	stmt.session.generation = stmt.mc.generation
	return nil
}
