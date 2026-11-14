// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package apps_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	mariadbgorm "github.com/aantich/go-mariadb/integrations/gorm"
	"github.com/jmoiron/sqlx"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixture struct {
	Host, User, Password, Database string
	Port                           int
	TLSCA                          string `json:"tls_ca"`
	Socket                         string `json:"socket"`
}

func config(t *testing.T, mode string) *mariadb.Config {
	t.Helper()
	path := os.Getenv("MARIADB_VALIDATION_CONFIG")
	if path == "" {
		t.Skip("application fixture not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Database != "go_mariadb_validation" {
		t.Fatal("refusing an application fixture outside the reserved schema")
	}
	c := mariadb.NewConfig()
	c.User, c.Passwd, c.DBName = f.User, f.Password, f.Database
	c.Net, c.Addr = "tcp", fmt.Sprintf("%s:%d", f.Host, f.Port)
	if f.Host == "" && f.Socket != "" {
		c.Net, c.Addr = "unix", f.Socket
	}
	c.ParseTime, c.Loc = true, time.UTC
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	c.Params = map[string]string{"time_zone": "'+00:00'"}
	if mode == "tls" || mode == "tls_compressed" {
		pem, err := os.ReadFile(f.TLSCA)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("invalid application fixture CA")
		}
		c.TLS = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"}
	}
	if mode == "compressed" || mode == "tls_compressed" {
		if err := c.Apply(mariadb.EnableCompression(true)); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func pool(t *testing.T, c *mariadb.Config) *sql.DB {
	t.Helper()
	connector, err := mariadb.NewConnector(c)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func orm(t *testing.T, db *sql.DB, translated bool) *gorm.DB {
	t.Helper()
	g, err := gorm.Open(mariadbgorm.New(mariadbgorm.Config{Conn: db}), &gorm.Config{
		PrepareStmt: true, TranslateError: translated, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Close GORM's statement cache before the caller-owned sql.DB cleanup.
	if cache, ok := g.ConnPool.(*gorm.PreparedStmtDB); ok {
		t.Cleanup(cache.Close)
	}
	return g
}

func extended(db *sql.DB) *sqlx.DB {
	sqlx.BindDriver("mariadb", sqlx.QUESTION)
	return sqlx.NewDb(db, "mariadb")
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(query, err)
	}
}

func scalar(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), query).Scan(&n); err != nil || n != want {
		t.Fatal("application state", n, want, err)
	}
}

func TestApplicationProvenance(t *testing.T) {
	cfg := config(t, "plain")
	if err := cfg.Apply(mariadb.WithSessionTracking(true)); err != nil {
		t.Fatal(err)
	}
	db := pool(t, cfg)
	// Verify the public DSN constructor as well as the existing-pool path used
	// by the workflow tests. A typed-nil Conn must not mask this configuration.
	g, err := gorm.Open(mariadbgorm.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	dsnPool, err := g.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer dsnPool.Close()
	if err := dsnPool.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var version string
	if err := db.QueryRowContext(t.Context(), "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := mariadb.InspectSession(t.Context(), conn); err != nil {
		t.Fatal("dedicated driver identity", err)
	}
	marker, _ := json.Marshal(map[string]string{"server_version": version, "driver_module": "github.com/aantich/go-mariadb", "registration": "mariadb"})
	t.Log("MARIADB_APPLICATION " + string(marker))
}
