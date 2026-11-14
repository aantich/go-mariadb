// MariaDB validation exercises the local fork through its public database/sql
// API. The module still has its inherited import path; Go resolves it to this
// checkout, not to a downloaded upstream driver.
package validation

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

	mysql "github.com/aantich/go-mariadb"
)

type fixtureConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Socket   string   `json:"socket"`
	User     string   `json:"user"`
	Password string   `json:"password"`
	Database string   `json:"database"`
	TLSCA    string   `json:"tls_ca"`
	TLSCert  string   `json:"tls_cert"`
	TLSKey   string   `json:"tls_key"`
	Plugins  []string `json:"plugins"`
}

func fixtures(tb testing.TB) fixtureConfig {
	tb.Helper()
	path := os.Getenv("MARIADB_VALIDATION_CONFIG")
	if path == "" {
		tb.Skip("ENVIRONMENT: set MARIADB_VALIDATION_CONFIG; run validation/provision.py first")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	var c fixtureConfig
	if err := json.Unmarshal(data, &c); err != nil {
		tb.Fatal(err)
	}
	if c.Database != "go_mariadb_validation" {
		tb.Fatal("refusing to run destructive fixtures outside reserved Go schema")
	}
	return c
}

func driverConfig(tb testing.TB, mode string) *mysql.Config {
	tb.Helper()
	c := fixtures(tb)
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = c.User, c.Password, c.Database
	cfg.Net, cfg.Addr = "tcp", fmt.Sprintf("%s:%d", c.Host, c.Port)
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	cfg.Params = map[string]string{"time_zone": "'+00:00'"}
	switch mode {
	case "interpolated":
		cfg.InterpolateParams = true
	case "compressed":
		parsed, err := mysql.ParseDSN(cfg.FormatDSN() + "&compress=true")
		if err != nil {
			tb.Fatal(err)
		}
		cfg = parsed
	case "socket":
		cfg.Net, cfg.Addr = "unix", c.Socket
	case "tls", "x509":
		pem, err := os.ReadFile(c.TLSCA)
		if err != nil {
			tb.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			tb.Fatal("invalid fixture CA")
		}
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"}
		if mode == "x509" {
			cert, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
			if err != nil {
				tb.Fatal(err)
			}
			cfg.TLS.Certificates = []tls.Certificate{cert}
			cfg.User = "gomdb_x509"
		}
	}
	return cfg
}

func openConfig(tb testing.TB, cfg *mysql.Config) *sql.DB {
	tb.Helper()
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	tb.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		tb.Fatal(err)
	}
	return db
}

func openDB(tb testing.TB, mode string) *sql.DB {
	tb.Helper()
	return openConfig(tb, driverConfig(tb, mode))
}

func mustExec(tb testing.TB, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) sql.Result {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		tb.Fatalf("%s: %v", query, err)
	}
	return result
}

func scalar(tb testing.TB, db *sql.DB, query string, want string, args ...any) {
	tb.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		tb.Fatal(err)
	}
	if got != want {
		tb.Fatalf("%s: got %q, want %q", query, got, want)
	}
}
