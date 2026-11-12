//go:build !cgo || (!darwin && !linux)

// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package gssapi

import mariadb "github.com/aantich/go-mariadb"

func openNative(mariadb.GSSAPIRequest) (nativeContext, error) { return nil, ErrUnavailable }
