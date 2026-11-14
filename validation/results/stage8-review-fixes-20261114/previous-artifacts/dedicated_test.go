package mariadb

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/sha512"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"
)

func TestDedicatedRegistration(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "mariadb") || slices.Contains(sql.Drivers(), "mysql") {
		t.Fatalf("driver registration collision: %v", sql.Drivers())
	}
}

func TestDedicatedPARSEC(t *testing.T) {
	nonce := make([]byte, 32) // A final zero is significant, not a terminator.
	_, mc := newRWMockConn(0)
	payload := append([]byte{iEOF}, []byte("parsec\x00")...)
	payload = append(payload, nonce...)
	name, seed := mc.parseAuthSwitchData(payload, nil)
	if name != "parsec" || !bytes.Equal(seed, nonce) {
		t.Fatal("binary challenge changed")
	}
	p := &parsecPlugin{}
	auth := &AuthContext{password: "test-password"}
	if _, err := p.InitAuth(t.Context(), seed, auth); err != nil {
		t.Fatal(err)
	}
	salt := append([]byte{'P', 0}, bytes.Repeat([]byte{7}, 18)...)
	response, err := p.ContinuationAuth(t.Context(), salt, seed, auth)
	if err != nil || len(response) != 96 {
		t.Fatalf("response length=%d, err=%v", len(response), err)
	}
	derived, err := pbkdf2.Key(sha512.New, auth.Password(), salt[2:], 1024, 32)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(derived)
	message := append(slices.Clone(nonce), response[:32]...)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, response[32:]) {
		t.Fatal("invalid PARSEC signature")
	}
	if _, err := p.ContinuationAuth(t.Context(), salt, seed, auth); err == nil {
		t.Fatal("second continuation accepted")
	}
	for _, invalid := range [][]byte{nil, {'X', 0}, append([]byte{'P', 11}, salt[2:]...)} {
		p := &parsecPlugin{}
		p.InitAuth(t.Context(), seed, auth)
		if _, err := p.ContinuationAuth(t.Context(), invalid, seed, auth); err == nil {
			t.Fatal("invalid salt accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (&parsecPlugin{}).InitAuth(ctx, seed, auth); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDedicatedUnsignedResult(t *testing.T) {
	r := &mysqlResult{affectedRows: []int64{math.MinInt64}, insertIds: []int64{-1}}
	if _, err := r.LastInsertId(); !errors.Is(err, ErrResultOverflow) {
		t.Fatal(err)
	}
	if _, err := r.RowsAffected(); !errors.Is(err, ErrResultOverflow) {
		t.Fatal(err)
	}
	if r.LastInsertIDUnsigned() != math.MaxUint64 || r.RowsAffectedUnsigned() != 1<<63 {
		t.Fatal("lost unsigned value")
	}
	ids := r.AllLastInsertIDsUnsigned()
	ids[0] = 0
	if r.LastInsertIDUnsigned() != math.MaxUint64 {
		t.Fatal("result slice aliases storage")
	}
}

func TestDedicatedBulkEncoding(t *testing.T) {
	_, mc := newRWMockConn(0)
	stmt := &mysqlStmt{mc: mc, id: 42, paramCount: 2}
	packet, err := stmt.encodeBulk([][]any{{uint64(math.MaxUint64), "a"}, {nil, BulkDefault}})
	if err != nil {
		t.Fatal(err)
	}
	if packet[4] != 0xfa || binary.LittleEndian.Uint32(packet[5:]) != 42 || binary.LittleEndian.Uint16(packet[9:]) != 128 {
		t.Fatal("invalid bulk header")
	}
	if binary.LittleEndian.Uint16(packet[11:]) != uint16(fieldTypeLongLong)|0x8000 {
		t.Fatal("unsigned type flag lost")
	}
	for _, rows := range [][][]any{{{1}}, {{int64(1), "a"}, {uint64(1), "b"}}, {{1, BulkIndicator(99)}}, {{1, make(chan int)}}} {
		if _, err := stmt.encodeBulk(rows); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	mc.maxAllowedPacket = 30
	if _, err := stmt.encodeBulk([][]any{{1, "too large for the packet budget"}}); !errors.Is(err, ErrPktTooLarge) {
		t.Fatal(err)
	}
}

func TestDedicatedBudgets(t *testing.T) {
	for _, option := range []Option{WithReceiveLimit(0), WithMetadataLimit(0), WithAuthenticationLimit(-1)} {
		if err := NewConfig().Apply(option); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	conn, mc := newRWMockConn(0)
	mc.cfg.Apply(WithReceiveLimit(4))
	conn.data = makePacket(0, []byte{1, 2, 3, 4, 5})
	if _, err := mc.readPacket(); !errors.Is(err, ErrPktTooLarge) || !mc.closed.Load() {
		t.Fatalf("limit: %v", err)
	}
	// A peer's UINT64 length must not overflow int and create a valid slice.
	if _, _, _, err := readLengthEncodedString([]byte{0xfe, 255, 255, 255, 255, 255, 255, 255, 255}); err == nil {
		t.Fatal("overflow length accepted")
	}
}

func TestDedicatedBulkExactBudget(t *testing.T) {
	_, mc := newRWMockConn(0)
	stmt := &mysqlStmt{mc: mc, id: 1, paramCount: 2}
	mc.maxAllowedPacket = 23 // 7 header + 4 types + 2 indicators + 8 integer + 2 string.
	packet, err := stmt.encodeBulk([][]any{{uint64(1), "a"}})
	if err != nil || len(packet)-4 != 23 {
		t.Fatalf("fitting batch rejected: bytes=%d err=%v", len(packet)-4, err)
	}
	mc.maxAllowedPacket--
	if _, err := stmt.encodeBulk([][]any{{uint64(1), "a"}}); !errors.Is(err, ErrPktTooLarge) {
		t.Fatal(err)
	}
	for _, test := range []struct{ n, want int }{{250, 251}, {251, 254}, {65535, 65538}, {65536, 65540}, {0x1000000, 0x1000009}} {
		if got := bulkStringSize(test.n); got != test.want {
			t.Fatalf("length %d: %d != %d", test.n, got, test.want)
		}
	}
}

func FuzzDedicatedLengthEncoding(f *testing.F) {
	for _, seed := range [][]byte{{0}, {0xfc}, {0xfe, 255, 255, 255, 255, 255, 255, 255, 255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		value, _, n, err := readLengthEncodedString(data)
		if err == nil && (n > len(data) || len(value) > len(data)) {
			t.Fatal("invalid accepted length")
		}
	})
}
