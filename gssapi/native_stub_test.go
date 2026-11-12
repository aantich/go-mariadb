//go:build !cgo || (!darwin && !linux)

// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package gssapi

import (
	"errors"
	"testing"

	mariadb "github.com/aantich/go-mariadb"
)

func TestUnavailableProvider(t *testing.T) {
	provider, err := NewClient(t.Context(), mariadb.GSSAPIRequest{ServicePrincipal: "mariadb/localhost@REALM"})
	if provider != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("unsupported native provider did not report ErrUnavailable")
	}
}
