// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"strings"
)

const (
	// SessionTrackSystemVariables contains length-encoded name/value pairs.
	SessionTrackSystemVariables byte = iota
	// SessionTrackSchema contains the server-reported current schema.
	SessionTrackSchema
	// SessionTrackStateChange is MariaDB's ASCII state-change flag.
	SessionTrackStateChange
	// SessionTrackGTIDs contains the server's encoded GTID observation.
	SessionTrackGTIDs
	// SessionTrackTransactionCharacteristics contains transaction characteristics.
	SessionTrackTransactionCharacteristics
	// SessionTrackTransactionState contains the transaction state description.
	SessionTrackTransactionState
)

// SessionChange is one server-reported tracking block. Kind is the MariaDB
// SESSION_TRACK_* wire identifier (0: variables, 1: schema, 2: state change,
// 3: GTIDs, 4: transaction characteristics, 5: transaction state). Data is an
// owned copy of the block payload, including unknown types for forward use.
type SessionChange struct {
	Kind byte
	Data []byte
}

// SessionSnapshot contains observations, not a complete session-state mirror.
// ObservedVariables and ObservedSchema are the latest values actually reported
// by the server; they may be incomplete or stale if SQL changes tracker settings.
// Changes belongs to the last successfully decoded OK/terminator. Generation
// advances at reset and Revision counts tracked responses within that generation.
// All maps/slices are owned by the caller. Status is the last wire status mask.
type SessionSnapshot struct {
	Generation        uint64
	Revision          uint64
	Status            uint16
	ObservedSchema    string
	ObservedVariables map[string]string
	Changes           []SessionChange
}

type sessionObservation struct {
	SessionSnapshot
	variableBytes int
}

// WithSessionTracking negotiates MariaDB session-state tracking. Configure which
// facts the server reports through the ordinary session_track_* Config.Params.
// This option alone does not change server tracking variables or enable reset.
func WithSessionTracking(enabled bool) Option {
	return func(c *Config) error { c.sessionTracking = enabled; return nil }
}

// WithSessionTrackLimit bounds each tracking payload and the retained variable
// dictionary independently. The default is 64 KiB, with at most 1,024 blocks and
// variable names. Oversized/malformed input retires the connection.
func WithSessionTrackLimit(bytes int) Option {
	return func(c *Config) error {
		if bytes < 1 {
			return errors.New("mariadb: session tracking limit must be positive")
		}
		c.sessionTrackLimit = bytes
		return nil
	}
}

// InspectSession copies observations from a pinned connection without sending
// SQL. Close active rows before calling it, and keep using the same sql.Conn
// when associating observations with a request. Tracking must be negotiated.
func InspectSession(ctx context.Context, conn *sql.Conn) (SessionSnapshot, error) {
	var snapshot SessionSnapshot
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if conn == nil {
		return snapshot, errors.New("mariadb: nil SQL connection")
	}
	err := conn.Raw(func(raw any) error {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return errors.New("mariadb: InspectSession requires this driver's connection")
		}
		if err := mc.error(); err != nil {
			return err
		}
		if mc.capabilities&clientSessionTrack == 0 {
			return ErrUnsupportedCapability
		}
		if mc.session != nil {
			snapshot = mc.session.SessionSnapshot
			snapshot.ObservedVariables = maps.Clone(snapshot.ObservedVariables)
			snapshot.Changes = cloneChanges(snapshot.Changes)
		}
		snapshot.Generation, snapshot.Status = mc.generation, uint16(mc.status)
		return ctx.Err()
	})
	return snapshot, err
}

func cloneChanges(changes []SessionChange) []SessionChange {
	if len(changes) == 0 {
		return nil
	}
	copy := make([]SessionChange, len(changes))
	for i, change := range changes {
		copy[i] = SessionChange{change.Kind, append([]byte(nil), change.Data...)}
	}
	return copy
}

// consumeString rejects NULL and reserved/truncated length prefixes. Borrowed
// bytes must be copied before the next packet read if they outlive this parser.
func consumeString(data []byte) (value, rest []byte, err error) {
	n, null, prefix := checkedLengthEncodedInteger(data)
	if prefix == 0 || null || n > uint64(len(data)-prefix) {
		return nil, nil, ErrMalformPkt
	}
	return data[prefix : prefix+int(n)], data[prefix+int(n):], nil
}

func (mc *mysqlConn) readSessionTail(data []byte, status statusFlag) error {
	// MariaDB omits even the empty info string when no message/change exists.
	if len(data) == 0 {
		if status&statusSessionStateChanged != 0 {
			return mc.protocolError()
		}
		if mc.session != nil {
			mc.session.Changes = nil
		}
		return nil
	}
	_, rest, err := consumeString(data) // human-readable info, not retained
	if err != nil {
		return mc.protocolError()
	}
	if status&statusSessionStateChanged == 0 {
		if len(rest) != 0 {
			return mc.protocolError()
		}
		if mc.session != nil {
			mc.session.Changes = nil
		}
		return nil
	}
	payload, trailing, err := consumeString(rest)
	if err != nil || len(trailing) != 0 {
		return mc.protocolError()
	}
	limit := 64 << 10
	if mc.cfg != nil && mc.cfg.sessionTrackLimit > 0 {
		limit = mc.cfg.sessionTrackLimit
	}
	if len(payload) > limit {
		return mc.protocolError()
	}
	var changes []SessionChange
	updates := make(map[string]string)
	var schema string
	var schemaSeen bool
	for len(payload) != 0 {
		if len(changes) >= 1024 {
			return mc.protocolError()
		}
		kind := payload[0]
		block, next, err := consumeString(payload[1:])
		if err != nil {
			return mc.protocolError()
		}
		payload = next
		switch kind {
		case SessionTrackSystemVariables:
			if len(block) == 0 {
				return mc.protocolError()
			}
			for fields := block; len(fields) != 0; {
				name, remaining, err := consumeString(fields)
				if err != nil || len(name) == 0 {
					return mc.protocolError()
				}
				value, remaining, err := consumeString(remaining)
				if err != nil {
					return mc.protocolError()
				}
				updates[strings.ToLower(string(name))] = string(value)
				if len(updates) > 1024 {
					return mc.protocolError()
				}
				fields = remaining
			}
		case SessionTrackSchema, SessionTrackTransactionCharacteristics, SessionTrackTransactionState:
			value, remaining, err := consumeString(block)
			if err != nil || len(remaining) != 0 {
				return mc.protocolError()
			}
			if kind == SessionTrackSchema {
				schema, schemaSeen = string(value), true
			}
		case SessionTrackStateChange:
			// MariaDB stores one ASCII flag directly, without a nested length.
			if len(block) != 1 || (block[0] != '0' && block[0] != '1') {
				return mc.protocolError()
			}
		case SessionTrackGTIDs:
			if len(block) == 0 {
				return mc.protocolError()
			}
			if block[0] == 0 { // Known textual GTID encoding; retain future encodings raw.
				_, remaining, err := consumeString(block[1:])
				if err != nil || len(remaining) != 0 {
					return mc.protocolError()
				}
			}
		}
		changes = append(changes, SessionChange{kind, append([]byte(nil), block...)})
	}
	if mc.session == nil {
		mc.session = &sessionObservation{}
	}
	state := mc.session
	bytes, names := state.variableBytes, len(state.ObservedVariables)
	// Remove replaced values first, then admit new bytes against remaining
	// capacity. This avoids integer overflow and permits equal-size replacement
	// of a full dictionary regardless of Go's randomized map iteration order.
	for key := range updates {
		old, present := state.ObservedVariables[key]
		if !present {
			names++
		}
		bytes -= len(old)
	}
	if names > 1024 {
		return mc.protocolError()
	}
	for key, value := range updates {
		if _, present := state.ObservedVariables[key]; !present {
			if len(key) > limit-bytes {
				return mc.protocolError()
			}
			bytes += len(key)
		}
		if len(value) > limit-bytes {
			return mc.protocolError()
		}
		bytes += len(value)
	}
	if len(updates) != 0 && state.ObservedVariables == nil {
		state.ObservedVariables = make(map[string]string, len(updates))
	}
	for key, value := range updates {
		state.ObservedVariables[key] = value
	}
	state.variableBytes = bytes
	if schemaSeen {
		state.ObservedSchema = schema
	}
	state.Changes = changes
	state.Revision++
	return nil
}
