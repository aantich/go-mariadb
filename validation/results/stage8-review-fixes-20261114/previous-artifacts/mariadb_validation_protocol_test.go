//go:build mariadb_validation

package mariadb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// A malformed peer must produce an error, never a process panic. Recovery here
// records an ordinary failing test and lets the baseline retain every finding.
// It does not convert a panic into an accepted or expected failure.
func requireProtocolError(t *testing.T, run func() error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("driver panicked on peer-controlled bytes: %v", p)
		}
	}()
	if err := run(); err == nil {
		t.Error("malformed protocol payload was accepted")
	}
}

func TestMariaDBMalformedGreeting(t *testing.T) {
	caps := clientProtocol41 | clientSecureConn | clientPluginAuth | clientPluginAuthLenEncClientData
	full := authTestHandshake("mysql_native_password", caps)[4:]
	// Every proper greeting prefix is truncated at a protocol boundary. The
	// final plugin-name bytes may be omitted legally, so stop before that field.
	for n := 1; n < min(50, len(full)); n++ {
		t.Run(fmt.Sprintf("prefix_%02d", n), func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			conn.maxReads = 2
			mc.capabilities = clientDeprecateEOF
			conn.data = makePacket(0, full[:n])
			requireProtocolError(t, func() error { _, _, _, _, err := mc.readHandshakePacket(); return err })
		})
	}
}

func TestMariaDBMalformedOK(t *testing.T) {
	for _, data := range [][]byte{{0}, {0, 0}, {0, 0, 0}, {0, 0, 0, 2}, {0, 0xfc}, {0, 0xfd, 0}, {0, 0xfe, 0, 0}} {
		t.Run(fmt.Sprintf("%x", data), func(t *testing.T) {
			_, mc := newRWMockConn(0)
			requireProtocolError(t, func() error { return mc.clearResult().handleOkPacket(data) })
		})
	}
}

func TestMariaDBMalformedMetadataHeader(t *testing.T) {
	for _, data := range [][]byte{{1}, {0xfc}, {0xfc, 1}, {0xfd, 1, 0}, {0xfe, 1, 0, 0}, {0xff, 1}} {
		t.Run(fmt.Sprintf("%x", data), func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			mc.extCapabilities = clientCacheMetadata
			conn.data = makePacket(0, data)
			requireProtocolError(t, func() error { _, _, err := mc.clearResult().readResultSetHeaderPacket(); return err })
		})
	}
}

func TestMariaDBMalformedColumn(t *testing.T) {
	// Six empty length-encoded identifiers followed by the fixed metadata.
	full := append([]byte{0, 0, 0, 0, 0, 0, 12}, bytes.Repeat([]byte{0}, 12)...)
	for n := 6; n < len(full); n++ {
		t.Run(fmt.Sprintf("prefix_%02d", n), func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			conn.maxReads = 2
			mc.capabilities = clientDeprecateEOF
			conn.data = makePacket(0, full[:n])
			requireProtocolError(t, func() error { _, err := mc.readColumns(1, nil); return err })
		})
	}
}

func TestMariaDBReceiveLimit(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 64
	conn.data = makePacket(0, bytes.Repeat([]byte{'x'}, 65))
	if _, err := mc.readPacket(); err == nil {
		t.Fatal("65-byte incoming payload accepted with maxAllowedPacket=64; receive limit is not enforced")
	}
}

func TestMariaDBNegotiatedCapabilities(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.initCapabilities(clientProtocol41|clientSecureConn|clientPluginAuth, clientCacheMetadata|clientExtendedMetadata|clientStmtBulkOperations)
	if mc.extCapabilities != clientCacheMetadata {
		t.Fatalf("unexpected inherited negotiation: %#x", mc.extCapabilities)
	}
	header, err := mc.handshakeResponseHeader()
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(header[32:36]); got != uint32(clientCacheMetadata) {
		t.Fatalf("MariaDB high capability word = %#x", got)
	}
}

// Metadata omission is valid only when a matching statement schema exists.
// Accepting an empty cache loses the declared result shape before row decoding.
func TestMariaDBMissingCachedMetadata(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.capabilities = clientDeprecateEOF
	mc.extCapabilities = clientCacheMetadata
	conn.queuedReplies = [][]byte{makePacket(1, []byte{1, 0})}
	stmt := &mysqlStmt{mc: mc, id: 1}
	requireProtocolError(t, func() error { _, err := stmt.Query(nil); return err })
}
