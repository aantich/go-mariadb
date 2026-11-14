//go:build cgo && (darwin || linux)

// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package gssapi

/*
#cgo darwin LDFLAGS: -framework GSS
#cgo linux LDFLAGS: -lgssapi_krb5
#include "native.h"
*/
import "C"

import (
	"fmt"
	"unsafe"

	mariadb "github.com/aantich/go-mariadb"
)

type native struct{ state *C.struct_mariadb_gss_context }

func openNative(policy mariadb.GSSAPIRequest) (nativeContext, error) {
	name := []byte(policy.ServicePrincipal)
	var state *C.struct_mariadb_gss_context
	var major, minor C.uint32_t
	var negotiate C.int
	if policy.Negotiate {
		negotiate = 1
	}
	status := C.mariadb_gss_open(unsafe.Pointer(unsafe.SliceData(name)), C.size_t(len(name)), negotiate, &state, &major, &minor)
	if status != 0 {
		return nil, nativeError(status, major, minor)
	}
	return &native{state}, nil
}

func nativeError(status C.int, major, minor C.uint32_t) error {
	// Native prose can include identities, cache paths and challenge contents.
	// Numeric provider status preserves diagnostics without recording those.
	return fmt.Errorf("mariadb/gssapi: provider status %d (major %d, minor %d)", status, major, minor)
}

func (n *native) step(token []byte) ([]byte, bool, error) {
	var output unsafe.Pointer
	var length C.size_t
	var complete C.int
	var major, minor C.uint32_t
	status := C.mariadb_gss_step(n.state, unsafe.Pointer(unsafe.SliceData(token)), C.size_t(len(token)), &output, &length, &complete, &major, &minor)
	if status != 0 {
		return nil, false, nativeError(status, major, minor)
	}
	return C.GoBytes(output, C.int(length)), complete != 0, nil
}

func (n *native) close() {
	C.mariadb_gss_close(n.state)
	n.state = nil
}

func nativeContexts() uint32 { return uint32(C.mariadb_gss_live_contexts()) }
