//go:build cgo && (darwin || linux)

// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package gssapi

import (
	"os"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

func TestNativeContextCleanup(t *testing.T) {
	principal := os.Getenv("GO_MARIADB_AUTH_GSSAPI_PRINCIPAL")
	if principal == "" {
		if os.Getenv("MARIADB_VALIDATION_CONFIG") != "" {
			t.Fatal("isolated Kerberos fixture required")
		}
		t.Skip("isolated Kerberos fixture not configured")
	}
	for i := 0; i < 12; i++ {
		provider, err := NewClient(t.Context(), mariadb.GSSAPIRequest{ServicePrincipal: principal})
		if err != nil {
			t.Fatal(err)
		}
		if token, complete, err := provider.Step(t.Context(), nil); err != nil || complete || len(token) == 0 {
			t.Fatalf("initial token: complete=%v err=%v", complete, err)
		}
		if i%2 == 0 {
			if _, _, err := provider.Step(t.Context(), []byte("invalid AP-REP")); err == nil {
				t.Fatal("invalid server token accepted")
			}
		}
		_ = provider.Close()
		select {
		case <-provider.(*client).done:
		case <-time.After(2 * time.Second):
			t.Fatal("context worker leaked")
		}
		if nativeContexts() != 0 {
			t.Fatal("native context/credential lease leaked")
		}
	}
}
