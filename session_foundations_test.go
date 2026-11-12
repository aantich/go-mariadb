// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func trackedBlock(kind byte, payload []byte) []byte {
	return append(append([]byte{kind}, appendLengthEncodedInteger(nil, uint64(len(payload)))...), payload...)
}

func trackedOK(marker byte, payload []byte, status statusFlag) []byte {
	data := []byte{marker, 0, 0, byte(status), byte(status >> 8), 0, 0}
	data = append(data, 0) // optional info is present when state follows
	return append(data, append(appendLengthEncodedInteger(nil, uint64(len(payload))), payload...)...)
}

func TestSessionTrackingDecode(t *testing.T) {
	variables := appendLengthEncodedString(nil, "time_zone")
	variables = appendLengthEncodedString(variables, "+02:00")
	payload := trackedBlock(0, variables)
	payload = append(payload, trackedBlock(1, appendLengthEncodedString(nil, "go_mariadb_validation"))...)
	payload = append(payload, trackedBlock(2, []byte("1"))...)
	payload = append(payload, trackedBlock(4, appendLengthEncodedString(nil, "READ ONLY"))...)
	payload = append(payload, trackedBlock(5, appendLengthEncodedString(nil, "T_______"))...)
	payload = append(payload, trackedBlock(90, []byte{1, 2, 3})...)
	for _, path := range []string{"ok", "terminator", "drain", "row"} {
		t.Run(path, func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			mc.capabilities = clientSessionTrack | clientDeprecateEOF
			data := trackedOK(iEOF, payload, statusSessionStateChanged|statusInAutocommit)
			var err error
			switch path {
			case "ok":
				data[0] = iOK
				err = mc.clearResult().handleOkPacket(data)
			case "terminator":
				mc.status, err = mc.terminatorStatus(data)
			case "drain":
				conn.data = makePacket(0, data)
				err = mc.skipRows()
			case "row":
				conn.data = makePacket(0, data)
				rows := &textRows{mysqlRows{mc: mc}}
				_, err = rows.readRowPacket(false)
				if err == io.EOF {
					err = nil
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if mc.session.ObservedVariables["time_zone"] != "+02:00" || mc.session.ObservedSchema != "go_mariadb_validation" || len(mc.session.Changes) != 6 {
				t.Fatalf("incorrect observation: %+v", mc.session)
			}
			data[len(data)-1] = 255
			if mc.session.Changes[5].Data[2] != 3 {
				t.Fatal("tracker retained packet storage")
			}
			if err := mc.clearResult().handleOkPacket([]byte{0, 0, 0, 2, 0, 0, 0}); err != nil || len(mc.session.Changes) != 0 {
				t.Fatalf("empty MariaDB OK: %v", err)
			}
		})
	}
}

func TestSessionTrackingMalformed(t *testing.T) {
	valid := trackedOK(0, trackedBlock(0, appendLengthEncodedString(appendLengthEncodedString(nil, "sql_mode"), "STRICT_TRANS_TABLES")), statusSessionStateChanged)
	for prefix := range len(valid) {
		_, mc := newRWMockConn(0)
		mc.capabilities = clientSessionTrack
		if err := mc.clearResult().handleOkPacket(valid[:prefix]); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
			t.Fatalf("prefix %d: %v", prefix, err)
		}
	}
	for _, payload := range [][]byte{{0}, {0, 0xfb}, trackedBlock(0, []byte{1, 'x'}), trackedBlock(1, []byte{0, 1}), trackedBlock(2, []byte{1, '1'}), trackedBlock(5, []byte{0xfc})} {
		_, mc := newRWMockConn(0)
		mc.capabilities = clientSessionTrack
		if err := mc.clearResult().handleOkPacket(trackedOK(0, payload, statusSessionStateChanged)); !errors.Is(err, ErrMalformPkt) {
			t.Fatalf("accepted %x: %v", payload, err)
		}
	}
	_, mc := newRWMockConn(0)
	mc.capabilities = clientSessionTrack
	mc.cfg.sessionTrackLimit = 4
	if err := mc.clearResult().handleOkPacket(valid); !errors.Is(err, ErrMalformPkt) {
		t.Fatal("tracking byte limit not enforced")
	}
}

func TestSessionTrackingMultipleResults(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.capabilities = clientSessionTrack | clientDeprecateEOF | clientMultiStatements
	variables := trackedBlock(SessionTrackSystemVariables, appendLengthEncodedString(appendLengthEncodedString(nil, "time_zone"), "+02:00"))
	schema := trackedBlock(SessionTrackSchema, appendLengthEncodedString(nil, "go_mariadb_upstream"))
	first := trackedOK(iOK, variables, statusSessionStateChanged|statusMoreResultsExists)
	last := trackedOK(iOK, schema, statusSessionStateChanged|statusInAutocommit)
	conn.queuedReplies = [][]byte{append(makePacket(1, first), makePacket(2, last)...)}
	if err := mc.exec("SET time_zone='+02:00'; USE go_mariadb_upstream"); err != nil {
		t.Fatal(err)
	}
	if mc.status&statusMoreResultsExists != 0 || mc.session.ObservedVariables["time_zone"] != "+02:00" || mc.session.ObservedSchema != "go_mariadb_upstream" || len(mc.result.affectedRows) != 2 {
		t.Fatalf("incomplete multiple-result observations: %+v", mc.session)
	}
}

func TestSessionTrackingRetainedBudget(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.capabilities = clientSessionTrack
	mc.cfg.sessionTrackLimit = 20
	for _, key := range []string{"a", "b", "c"} {
		payload := trackedBlock(0, appendLengthEncodedString(appendLengthEncodedString(nil, key), "12345678"))
		err := mc.clearResult().handleOkPacket(trackedOK(0, payload, statusSessionStateChanged))
		if key == "c" && !errors.Is(err, ErrMalformPkt) {
			t.Fatal("cumulative variable budget ignored")
		}
		if key != "c" && err != nil {
			t.Fatal(err)
		}
	}
}

func extendedColumn(payload []byte) []byte {
	data := []byte{}
	for _, name := range []string{"def", "db", "table", "table", "v", "v"} {
		data = appendLengthEncodedString(data, name)
	}
	data = append(data, append(appendLengthEncodedInteger(nil, uint64(len(payload))), payload...)...)
	return append(data, 12, 63, 0, 36, 0, 0, 0, byte(fieldTypeString), 0, 0, 0, 0, 0)
}

func TestExtendedMetadataDecode(t *testing.T) {
	metadata := append([]byte{0}, appendLengthEncodedString(nil, "uuid")...)
	metadata = append(metadata, append([]byte{1}, appendLengthEncodedString(nil, "native")...)...)
	metadata = append(metadata, append([]byte{45}, appendLengthEncodedString(nil, "future")...)...)
	conn, mc := newRWMockConn(0)
	mc.extCapabilities = clientExtendedMetadata
	mc.capabilities = clientDeprecateEOF
	conn.data = makePacket(0, extendedColumn(metadata))
	columns, err := mc.readColumns(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if columns[0].databaseType() != "UUID" || columns[0].extended.format != "native" || len(columns[0].extended.attributes) != 3 {
		t.Fatalf("metadata=%+v", columns[0])
	}
	for _, payload := range [][]byte{{0}, {0, 0xfb}, {0, 1, 'a', 0, 1, 'b'}, {0xfc}, {1, 5, 'a'}} {
		conn, mc := newRWMockConn(0)
		mc.extCapabilities, mc.capabilities = clientExtendedMetadata, clientDeprecateEOF
		conn.data = makePacket(0, extendedColumn(payload))
		if _, err := mc.readColumns(1, nil); !errors.Is(err, ErrMalformPkt) || !mc.closed.Load() {
			t.Fatalf("accepted malformed attributes %x", payload)
		}
	}
}

func TestManagedResetPolicyClone(t *testing.T) {
	policy := SessionResetPolicy{Role: "role", InitSQL: []string{"DO 1"}}
	cfg := NewConfig()
	if err := cfg.Apply(WithSessionReset(policy), Charset("utf8mb4", "utf8mb4_general_ci")); err != nil {
		t.Fatal(err)
	}
	policy.InitSQL[0] = "DO 2"
	clone := cfg.Clone()
	clone.sessionReset.InitSQL[0], clone.charsets[0] = "DO 3", "latin1"
	if cfg.sessionReset.InitSQL[0] != "DO 1" || cfg.charsets[0] != "utf8mb4" {
		t.Fatal("mutable session policy/charset configuration is shared")
	}
	if _, err := NewConnector(cfg); err == nil {
		t.Fatal("managed reset accepted an unspecified schema")
	}
	cfg.DBName = "db"
	if _, err := NewConnector(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestManagedResetStatementGeneration(t *testing.T) {
	for _, arity := range []int{0, 1} {
		t.Run(fmt.Sprint(arity), func(t *testing.T) {
			conn, mc := newRWMockConn(0)
			mc.cfg.sessionReset = &SessionResetPolicy{}
			mc.capabilities = clientDeprecateEOF
			mc.generation = 1
			stmt := &mysqlStmt{mc: mc, id: 7, session: &statementSession{queryText: "DO 1"}}
			reply := []byte{0, 8, 0, 0, 0, 0, 0, byte(arity), 0, 0, 0, 0}
			conn.queuedReplies = [][]byte{makePacket(1, reply)}
			if arity != 0 {
				conn.queuedReplies[0] = append(conn.queuedReplies[0], makePacket(2, []byte{0})...)
			}
			err := stmt.ensurePrepared()
			if arity != 0 {
				if err == nil {
					t.Fatal("changed parameter arity accepted")
				}
				return
			}
			if err != nil || stmt.id != 8 || stmt.session.generation != 1 {
				t.Fatalf("reprepare: %+v %v", stmt, err)
			}
			stale := &mysqlStmt{mc: mc, id: 8, session: &statementSession{generation: 0}}
			before := len(conn.written)
			if err := stale.Close(); err != nil || len(conn.written) != before {
				t.Fatal("stale handle emitted CLOSE")
			}
		})
	}
}

func TestManagedResetCancellation(t *testing.T) {
	for _, phase := range []string{"reset_write", "reset_reply", "charset_write", "charset_reply", "role_reply", "schema_reply", "params_reply", "init_reply"} {
		t.Run(phase, func(t *testing.T) {
			client, peer := net.Pipe()
			defer peer.Close()
			cfg := NewConfig()
			cfg.DBName = "db"
			cfg.Params = map[string]string{"time_zone": "'+00:00'"}
			cfg.Apply(WithSessionReset(SessionResetPolicy{InitSQL: []string{"DO 1"}}))
			mc := &mysqlConn{buf: newBuffer(), cfg: cfg, netConn: client, rawConn: client, closech: make(chan struct{}), maxAllowedPacket: defaultMaxAllowedPacket, capabilities: clientDeprecateEOF}
			mc.startWatcher()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				for _, name := range []string{"reset", "charset", "role", "schema", "params", "init"} {
					if phase == name+"_write" {
						return
					}
					if readFaultCommand(peer) != nil || phase == name+"_reply" {
						return
					}
					if _, err := peer.Write(makePacket(1, []byte{0, 0, 0, 2, 0, 0, 0})); err != nil {
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err := mc.resetSession(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || !mc.closed.Load() {
				t.Fatalf("phase=%s error=%v retired=%v", phase, err, mc.closed.Load())
			}
			peer.Close()
			<-finished
		})
	}
}

// This seed-driven fuzz target is also exercised as ordinary correctness tests;
// running a timed fuzz campaign is not required for the no-benchmark workflow.
func FuzzSessionTracking(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, 0, 0, 0})
	f.Add(trackedOK(0, trackedBlock(1, appendLengthEncodedString(nil, "db")), statusSessionStateChanged))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, mc := newRWMockConn(0)
		mc.capabilities = clientSessionTrack
		_ = mc.clearResult().handleOkPacket(data)
	})
}

func FuzzExtendedMetadata(f *testing.F) {
	f.Add([]byte{0, 4, 'u', 'u', 'i', 'd'})
	f.Add([]byte{0, 0xfb})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		_, _, _, _ = parseColumnAttributes(data)
	})
}

func TestSessionTrackingCountLimits(t *testing.T) {
	for _, variables := range []bool{false, true} {
		var payload []byte
		for i := range 1025 {
			if variables {
				pair := appendLengthEncodedString(appendLengthEncodedString(nil, fmt.Sprintf("v%d", i)), "")
				payload = append(payload, trackedBlock(SessionTrackSystemVariables, pair)...)
			} else {
				payload = append(payload, trackedBlock(90, nil)...)
			}
		}
		_, mc := newRWMockConn(0)
		mc.capabilities = clientSessionTrack
		if err := mc.clearResult().handleOkPacket(trackedOK(0, payload, statusSessionStateChanged)); !errors.Is(err, ErrMalformPkt) {
			t.Fatalf("count limit not enforced (variables=%v): %v", variables, err)
		}
	}
}

func TestSessionTrackingFullDictionaryReplacement(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.capabilities = clientSessionTrack
	mc.cfg.sessionTrackLimit = 64
	set := func(a, b string) error {
		payload := trackedBlock(0, appendLengthEncodedString(appendLengthEncodedString(nil, "a"), a))
		payload = append(payload, trackedBlock(0, appendLengthEncodedString(appendLengthEncodedString(nil, "b"), b))...)
		return mc.clearResult().handleOkPacket(trackedOK(0, payload, statusSessionStateChanged))
	}
	if err := set(strings.Repeat("a", 20), strings.Repeat("b", 10)); err != nil {
		t.Fatal(err)
	}
	third := trackedBlock(0, appendLengthEncodedString(appendLengthEncodedString(nil, "c"), strings.Repeat("c", 31)))
	if err := mc.clearResult().handleOkPacket(trackedOK(0, third, statusSessionStateChanged)); err != nil {
		t.Fatal(err)
	}
	if mc.session.variableBytes != 64 {
		t.Fatal("fixture did not fill the retained dictionary")
	}
	if err := set(strings.Repeat("a", 10), strings.Repeat("b", 20)); err != nil {
		t.Fatalf("equivalent replacement rejected: %v", err)
	}
	if mc.session.variableBytes != 64 {
		t.Fatalf("dictionary byte accounting: %d", mc.session.variableBytes)
	}
}
