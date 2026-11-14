// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package apps_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	"gorm.io/gorm"
)

type application struct {
	exec func(context.Context, string, ...any) error
	rows func(context.Context, string) (*sql.Rows, error)
	// prepareID warms a framework-specific handle and returns an operation that
	// reuses it. The test owns cleanup through t.Cleanup, like the other clients.
	prepareID func(*testing.T) func(context.Context) (uint64, error)
}

func client(t *testing.T, name string, db *sql.DB) application {
	t.Helper()
	switch name {
	case "gorm":
		g := orm(t, db, true)
		return application{exec: func(ctx context.Context, q string, args ...any) error {
			return g.WithContext(ctx).Exec(q, args...).Error
		}, rows: func(ctx context.Context, q string) (*sql.Rows, error) { return g.WithContext(ctx).Raw(q).Rows() }, prepareID: func(t *testing.T) func(context.Context) (uint64, error) {
			cache, ok := g.ConnPool.(*gorm.PreparedStmtDB)
			if !ok {
				t.Fatal("GORM prepared cache is missing")
			}
			const query = "SELECT CONNECTION_ID()"
			var original *sql.Stmt
			return func(ctx context.Context) (uint64, error) {
				var id uint64
				err := g.WithContext(ctx).Raw(query).Scan(&id).Error
				cached, ok := cache.Stmts.Get(query)
				if !ok || cached.Stmt == nil {
					t.Fatal("GORM query bypassed its prepared cache")
				}
				if original != nil && original != cached.Stmt {
					t.Fatal("GORM did not reuse its warmed handle")
				}
				original = cached.Stmt
				return id, err
			}
		}}
	case "sqlx":
		x := extended(db)
		return application{exec: func(ctx context.Context, q string, args ...any) error {
			_, err := x.ExecContext(ctx, q, args...)
			return err
		}, rows: func(ctx context.Context, q string) (*sql.Rows, error) {
			r, err := x.QueryxContext(ctx, q)
			if err != nil {
				return nil, err
			}
			return r.Rows, nil
		}, prepareID: func(t *testing.T) func(context.Context) (uint64, error) {
			stmt, err := x.PrepareNamedContext(t.Context(), "SELECT CONNECTION_ID() WHERE :enabled=1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stmt.Close() })
			return func(ctx context.Context) (uint64, error) {
				var id uint64
				err := stmt.GetContext(ctx, &id, map[string]any{"enabled": 1})
				return id, err
			}
		}}
	default:
		return application{exec: func(ctx context.Context, q string, args ...any) error {
			_, err := db.ExecContext(ctx, q, args...)
			return err
		}, rows: func(ctx context.Context, q string) (*sql.Rows, error) { return db.QueryContext(ctx, q) }, prepareID: func(t *testing.T) func(context.Context) (uint64, error) {
			stmt, err := db.PrepareContext(t.Context(), "SELECT CONNECTION_ID()")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stmt.Close() })
			return func(ctx context.Context) (uint64, error) {
				var id uint64
				err := stmt.QueryRowContext(ctx).Scan(&id)
				return id, err
			}
		}}
	}
}

func TestApplicationLifecycle(t *testing.T) {
	for _, name := range []string{"sql", "sqlx", "gorm"} {
		t.Run(name, func(t *testing.T) {
			db := pool(t, config(t, "plain"))
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			app := client(t, name, db)
			t.Run("pool_wait_cancellation", func(t *testing.T) {
				held, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				err = app.exec(ctx, "DO 1")
				cancel()
				held.Close()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("pool wait cancellation", err)
				}
				if db.Stats().InUse != 0 || db.Stats().WaitCount == 0 {
					t.Fatal("pool lease/wait", db.Stats())
				}
			})
			t.Run("active_query_cancellation", func(t *testing.T) {
				var before, after uint64
				if err := db.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&before); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				err := app.exec(ctx, "DO SLEEP(30)")
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("active cancellation", err)
				}
				if err := db.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&after); err != nil || before == after {
					t.Fatal("failed connection reused", before, after, err)
				}
			})
			t.Run("early_rows_close", func(t *testing.T) {
				for range 8 {
					rows, err := app.rows(t.Context(), "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3")
					if err != nil {
						t.Fatal(err)
					}
					if !rows.Next() {
						t.Fatal(rows.Err())
					}
					var n int
					if err := rows.Scan(&n); err != nil || n != 1 {
						t.Fatal(n, err)
					}
					if err := rows.Close(); err != nil {
						t.Fatal(err)
					}
					if db.Stats().InUse != 0 {
						t.Fatal("Rows retained a pool lease", db.Stats())
					}
					if err := app.exec(t.Context(), "DO 1"); err != nil {
						t.Fatal("response not drained", err)
					}
				}
			})
			t.Run("prepared_handle_expiry", func(t *testing.T) {
				if app.prepareID == nil {
					t.Fatal("framework prepared client is missing")
				}
				queryID := app.prepareID(t)
				before, err := queryID(t.Context())
				if err != nil || before == 0 {
					t.Fatal("warm prepared handle", before, err)
				}
				// Expire the physical connection, then reuse the same sql/sqlx
				// handle or GORM cache. No reaper or scheduler sleep is needed.
				db.SetConnMaxLifetime(time.Nanosecond)
				after, err := queryID(t.Context())
				if err != nil || after == 0 || before == after {
					t.Fatal("framework expiry/reprepare", before, after, err)
				}
				db.SetConnMaxLifetime(0)
			})
			t.Run("concurrent_requests", func(t *testing.T) {
				exec(t, db, "DROP TABLE IF EXISTS go_app_counter")
				exec(t, db, "CREATE TABLE go_app_counter(id INT PRIMARY KEY,n INT) ENGINE=InnoDB")
				exec(t, db, "INSERT INTO go_app_counter VALUES(1,0)")
				defer db.ExecContext(context.Background(), "DROP TABLE go_app_counter")
				db.SetMaxOpenConns(2)
				db.SetMaxIdleConns(2)
				var workers sync.WaitGroup
				failures := make(chan error, 8)
				for range 8 {
					workers.Add(1)
					go func() {
						defer workers.Done()
						for range 4 {
							if err := app.exec(t.Context(), "UPDATE go_app_counter SET n=n+1 WHERE id=1"); err != nil {
								failures <- err
								return
							}
						}
					}()
				}
				workers.Wait()
				close(failures)
				for err := range failures {
					t.Fatal(err)
				}
				scalar(t, db, "SELECT n FROM go_app_counter WHERE id=1", 32)
				if db.Stats().InUse != 0 {
					t.Fatal("request leaked a lease", db.Stats())
				}
			})
		})
	}
}

func TestApplicationManagedSessions(t *testing.T) {
	cfg := config(t, "plain")
	if err := cfg.Apply(mariadb.WithSessionReset(mariadb.SessionResetPolicy{InitSQL: []string{"SET @go_app_init='ready'"}})); err != nil {
		t.Fatal(err)
	}
	db := pool(t, cfg)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	g := orm(t, db, true)
	if err := g.WithContext(t.Context()).Connection(func(tx *gorm.DB) error { return tx.Exec("SET @go_app_secret='private',@go_app_init='dirty'").Error }); err != nil {
		t.Fatal(err)
	}
	x := extended(db)
	conn, err := x.Connx(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var secret sql.NullString
	var init string
	if err := conn.QueryRowContext(t.Context(), "SELECT @go_app_secret,@go_app_init").Scan(&secret, &init); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()
	if secret.Valid || init != "ready" {
		t.Fatal("cross-framework session leak", secret, init)
	}
	for range 3 {
		var n int
		if err := g.WithContext(t.Context()).Raw("SELECT ?+1", 1).Scan(&n).Error; err != nil || n != 2 {
			t.Fatal("GORM cached statement/reset", n, err)
		}
	}
	if db.Stats().InUse != 0 {
		t.Fatal("managed lease leaked", db.Stats())
	}
}

func TestApplicationRestart(t *testing.T) {
	// The controller exposes this path only for its private server. No SQL can
	// request restarting a shared service, and the request carries no command.
	control := os.Getenv("GO_MARIADB_MATRIX_CONTROL")
	if control == "" {
		control = os.Getenv("GO_MARIADB_CDC_CONTROL")
	}
	if control == "" {
		t.Skip("owned application restart control not configured")
	}
	cfg := config(t, "plain")
	plain := pool(t, cfg)
	x := extended(pool(t, cfg))
	g := orm(t, pool(t, cfg), true)
	exec(t, plain, "DROP TABLE IF EXISTS go_app_restart")
	exec(t, plain, "CREATE TABLE go_app_restart(id INT PRIMARY KEY) ENGINE=InnoDB")
	exec(t, plain, "INSERT INTO go_app_restart VALUES(1)")
	defer plain.ExecContext(context.Background(), "DROP TABLE go_app_restart")
	stmt, err := plain.PrepareContext(t.Context(), "SELECT COUNT(*) FROM go_app_restart")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	named, err := x.PrepareNamedContext(t.Context(), "SELECT COUNT(*) FROM go_app_restart WHERE id>=:id")
	if err != nil {
		t.Fatal(err)
	}
	defer named.Close()
	var n int
	if err := stmt.QueryRowContext(t.Context()).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := named.GetContext(t.Context(), &n, map[string]any{"id": 1}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := g.WithContext(t.Context()).Raw("SELECT COUNT(*) FROM go_app_restart").Scan(&n).Error; err != nil || n != 1 {
		t.Fatal(n, err)
	}
	tx := g.WithContext(t.Context()).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("INSERT INTO go_app_restart VALUES(2)").Error; err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(control, "restart.done")
	os.Remove(done)
	if err := os.WriteFile(filepath.Join(control, "restart.request"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(35 * time.Second)
	for {
		if _, err := os.Stat(done); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned application restart deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := tx.Commit().Error; err == nil {
		t.Fatal("disconnected GORM transaction committed")
	}
	if err := stmt.QueryRowContext(t.Context()).Scan(&n); err != nil || n != 1 {
		t.Fatal("sql restart", n, err)
	}
	if err := named.GetContext(t.Context(), &n, map[string]any{"id": 1}); err != nil || n != 1 {
		t.Fatal("sqlx restart", n, err)
	}
	if err := g.WithContext(t.Context()).Raw("SELECT COUNT(*) FROM go_app_restart").Scan(&n).Error; err != nil || n != 1 {
		t.Fatal("GORM restart", n, err)
	}
	t.Log("APPLICATION_RESTART completed=1")
}
