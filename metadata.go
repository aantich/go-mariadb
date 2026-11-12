// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ColumnAttribute is an owned native metadata value. Kind 0 is the MariaDB data
// type name and kind 1 the format name. Unknown kinds are preserved unchanged.
type ColumnAttribute struct {
	Kind  uint64
	Value string
}

const (
	// ColumnAttributeTypeName identifies the native MariaDB type-name attribute.
	ColumnAttributeTypeName uint64 = iota
	// ColumnAttributeFormatName identifies a format such as JSON.
	ColumnAttributeFormatName
)

type columnExtension struct {
	nativeType string
	format     string
	attributes []ColumnAttribute
}

// ColumnMetadata describes a prepared result column without converting values.
// DatabaseType uses a native name when provided, otherwise the ordinary wire type.
// WireType/Flags/Collation retain their MariaDB protocol identifiers. Length is
// the server's maximum display length in bytes, not a measured value length.
type ColumnMetadata struct {
	Name         string
	DatabaseType string
	WireType     byte
	Flags        uint16
	Collation    uint16
	Length       uint32
	Decimals     byte
	Attributes   []ColumnAttribute
}

// WithExtendedMetadata requests MariaDB's native column attributes. It changes
// metadata parsing and ColumnTypes names, not value decoding or scanner types.
// An unavailable negotiated capability is an error during connection setup.
func WithExtendedMetadata(enabled bool) Option {
	return func(c *Config) error { c.extendedMetadata = enabled; return nil }
}

// Describe prepares query on a pinned connection and returns owned result
// metadata, then closes that transient statement. It does not execute query or
// bind parameters. Extended metadata must be enabled. Close active rows first.
// Native attributes describe the server's prepared result; later DDL may change
// them, so describe again when an application needs a fresh schema description.
func Describe(ctx context.Context, conn *sql.Conn, query string) (columns []ColumnMetadata, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("mariadb: nil SQL connection")
	}
	err = conn.Raw(func(raw any) (err error) {
		mc, ok := raw.(*mysqlConn)
		if !ok {
			return errors.New("mariadb: Describe requires this driver's connection")
		}
		if err := mc.error(); err != nil {
			return err
		}
		if mc.extCapabilities&clientExtendedMetadata == 0 {
			return ErrUnsupportedCapability
		}
		if err := mc.watchCancel(ctx); err != nil {
			return err
		}
		defer mc.finish()
		prepared, err := mc.Prepare(query)
		if err != nil {
			return err
		}
		stmt := prepared.(*mysqlStmt)
		defer func() {
			if closeErr := stmt.Close(); err == nil {
				err = closeErr
			}
		}()
		columns = make([]ColumnMetadata, len(stmt.columns))
		for i, field := range stmt.columns {
			var attributes []ColumnAttribute
			if field.extended != nil {
				attributes = append([]ColumnAttribute(nil), field.extended.attributes...)
			}
			columns[i] = ColumnMetadata{field.name, field.databaseType(), byte(field.fieldType), uint16(field.flags), field.charSet, field.length, field.decimals, attributes}
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return columns, nil
}

func (field *mysqlField) databaseType() string {
	if field.extended != nil {
		if field.extended.nativeType != "" {
			return field.extended.nativeType
		}
		if strings.EqualFold(field.extended.format, "json") {
			return "JSON"
		}
	}
	return field.typeDatabaseName()
}

func parseColumnAttributes(data []byte) (attributes []ColumnAttribute, nativeType, format string, err error) {
	return decodeColumnAttributes(data, true)
}

// decodeColumnAttributes validates the same wire representation for both callers.
// Discarding parameter/result definitions must not allocate owned values that
// will never be used. Common attribute IDs fit in a bit mask; only future, large
// IDs need a map for duplicate detection.
func decodeColumnAttributes(data []byte, retain bool) (attributes []ColumnAttribute, nativeType, format string, err error) {
	var smallSeen uint64
	var largeSeen map[uint64]struct{}
	count := 0
	for len(data) != 0 {
		kind, null, prefix := checkedLengthEncodedInteger(data)
		if prefix == 0 || null || count >= 1024 {
			return nil, "", "", ErrMalformPkt
		}
		if kind < 64 {
			mask := uint64(1) << kind
			if smallSeen&mask != 0 {
				return nil, "", "", ErrMalformPkt
			}
			smallSeen |= mask
		} else {
			if _, duplicate := largeSeen[kind]; duplicate {
				return nil, "", "", ErrMalformPkt
			}
			if largeSeen == nil {
				largeSeen = make(map[uint64]struct{})
			}
			largeSeen[kind] = struct{}{}
		}
		value, rest, err := consumeString(data[prefix:])
		if err != nil {
			return nil, "", "", err
		}
		if retain {
			text := string(value)
			attributes = append(attributes, ColumnAttribute{kind, text})
			switch kind {
			case ColumnAttributeTypeName:
				nativeType = strings.ToUpper(text)
			case ColumnAttributeFormatName:
				format = text
			}
		}
		count++
		data = rest
	}
	return attributes, nativeType, format, nil
}

// validateExtendedColumn consumes borrowed slices only. Parameter definitions and
// discarded result metadata have the same six standard strings, attributes and
// fixed tail as retained columns, but need no mysqlField slice or string copies.
func validateExtendedColumn(data []byte) error {
	for range 6 {
		n, err := skipLengthEncodedString(data)
		if err != nil {
			return ErrMalformPkt
		}
		data = data[n:]
	}
	attributes, tail, err := consumeString(data)
	if err != nil {
		return err
	}
	if _, _, _, err := decodeColumnAttributes(attributes, false); err != nil {
		return err
	}
	return validateColumnTail(tail)
}

// Both retained and discarded definitions require the twelve fixed bytes. A
// trailing default-value field is permitted by the column-definition protocol.
func validateColumnTail(data []byte) error {
	if len(data) < 13 || data[0] != 12 {
		return ErrMalformPkt
	}
	return nil
}
