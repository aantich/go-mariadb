// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package validation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	"github.com/aantich/go-mariadb/gssapi"
)

func externalAuthConfig(t *testing.T, user string, secure bool) *mariadb.Config {
	t.Helper()
	cfg := mariadb.NewConfig()
	cfg.User = user
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(os.Getenv("GO_MARIADB_AUTH_HOST"), os.Getenv("GO_MARIADB_AUTH_PORT"))
	cfg.DBName = "go_mariadb_validation"
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 2 * time.Second
	cfg.WriteTimeout = 2 * time.Second
	if secure {
		pem, err := os.ReadFile(os.Getenv("GO_MARIADB_AUTH_TLS_CA"))
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("invalid fixture CA")
		}
		cfg.TLS = &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	}
	return cfg
}

func externalAuthDB(t *testing.T, cfg *mariadb.Config) *sql.DB {
	t.Helper()
	connector, err := mariadb.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMariaDBExternalAuthentication(t *testing.T) {
	if os.Getenv("GO_MARIADB_AUTH_HOST") == "" {
		if os.Getenv("MARIADB_VALIDATION_CONFIG") != "" {
			t.Fatal("isolated PAM/Kerberos fixture required: use validation/auth_fixture.py")
		}
		t.Skip("isolated external authentication fixture not configured")
	}
	t.Log("EXTERNAL_AUTH " + os.Getenv("GO_MARIADB_AUTH_RECEIPT"))
	for _, mode := range []string{"password", "multiple", "wrong password", "wrong second factor", "plaintext rejected", "disabled", "callback error", "cancellation"} {
		t.Run("PAM/"+mode, func(t *testing.T) {
			user := "gomdb_pam"
			if mode == "multiple" || mode == "wrong second factor" {
				user = "gomdb_pam_multi"
			}
			cfg := externalAuthConfig(t, user, mode != "plaintext rejected")
			calls := 0
			marker := errors.New("prompt declined")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode != "disabled" {
				if err := cfg.Apply(mariadb.WithPAMDialog(mariadb.PAMDialogConfig{Prompt: func(ctx context.Context, p mariadb.PAMPrompt) (string, error) {
					calls++
					if mode == "callback error" {
						return "", marker
					}
					if mode == "cancellation" {
						cancel()
						<-ctx.Done()
						return "", ctx.Err()
					}
					switch {
					case p.Text == "Fixture password:" && !p.Echo && p.Number == 1:
						if mode == "wrong password" {
							return "wrong", nil
						}
						return os.Getenv("GO_MARIADB_AUTH_PAM_PASSWORD"), nil
					case p.Text == "Fixture code:" && p.Echo && p.Number == 2:
						if mode == "wrong second factor" {
							return "wrong", nil
						}
						return os.Getenv("GO_MARIADB_AUTH_PAM_CODE"), nil
					default:
						return "", errors.New("unexpected prompt")
					}
				}})); err != nil {
					t.Fatal(err)
				}
			}
			db := externalAuthDB(t, cfg)
			var got int
			err := db.QueryRowContext(ctx, "SELECT 42").Scan(&got)
			if mode == "password" || mode == "multiple" {
				want := 1
				if mode == "multiple" {
					want = 2
				}
				if err != nil || got != 42 || calls != want {
					t.Fatalf("query=%d calls=%d error=%v", got, calls, err)
				}
			} else {
				if err == nil {
					t.Fatal("rejected authentication succeeded")
				}
				if (mode == "plaintext rejected" || mode == "disabled") && (calls != 0 || !errors.Is(err, mariadb.ErrPAMPolicy)) {
					t.Fatalf("prompt policy bypassed: %v", err)
				}
				if mode == "callback error" && !errors.Is(err, marker) {
					t.Fatal(err)
				}
				if mode == "cancellation" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
		})
	}
	for _, mode := range []string{"TCP", "TLS", "wrong principal", "wrong identity", "missing cache", "disabled", "repeated connections"} {
		t.Run("GSSAPI/"+mode, func(t *testing.T) {
			if mode == "wrong identity" {
				t.Setenv("KRB5CCNAME", os.Getenv("GO_MARIADB_AUTH_GSSAPI_OTHER_CACHE"))
			}
			if mode == "missing cache" {
				t.Setenv("KRB5CCNAME", "FILE:"+filepath.Join(t.TempDir(), "missing.ccache"))
			}
			cfg := externalAuthConfig(t, "gomdb_gssapi", mode == "TLS")
			var acquired atomic.Int32
			if mode != "disabled" {
				spn := os.Getenv("GO_MARIADB_AUTH_GSSAPI_PRINCIPAL")
				if mode == "wrong principal" {
					spn = "mariadb/wrong@GO-MARIADB.TEST"
				}
				if err := cfg.Apply(mariadb.WithGSSAPI(mariadb.GSSAPIConfig{ServicePrincipal: spn, NewClient: func(ctx context.Context, r mariadb.GSSAPIRequest) (mariadb.GSSAPIClient, error) {
					acquired.Add(1)
					return gssapi.NewClient(ctx, r)
				}})); err != nil {
					t.Fatal(err)
				}
			}
			db := externalAuthDB(t, cfg)
			if mode == "repeated connections" {
				db.SetMaxIdleConns(0)
			}
			n := 1
			if mode == "repeated connections" {
				n = 12
			}
			for range n {
				var got string
				err := db.QueryRowContext(t.Context(), "SELECT CURRENT_USER()").Scan(&got)
				if mode == "TCP" || mode == "TLS" || mode == "repeated connections" {
					if err != nil || !strings.HasPrefix(got, "gomdb_gssapi@") {
						t.Fatalf("query identity incorrect: %v", err)
					}
				} else if err == nil {
					t.Fatal("rejected GSSAPI authentication succeeded")
				}
			}
			if (mode == "wrong principal" || mode == "disabled") && acquired.Load() != 0 {
				t.Fatal("credentials acquired before policy check")
			}
			if mode == "repeated connections" && acquired.Load() != 12 {
				t.Fatal("contexts reused across connections")
			}
		})
	}
}
