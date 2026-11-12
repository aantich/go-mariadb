// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import "fmt"

// WithReceiveLimit limits the total payload bytes in one incoming logical
// message, including messages split over several wire packets. A positive value
// overrides the default MaxAllowedPacket bound without restricting outgoing SQL.
// Oversized input retires the connection before reading the offending body.
func WithReceiveLimit(bytes int) Option {
	return func(c *Config) error {
		if bytes < 1 {
			return fmt.Errorf("mariadb: receive limit must be positive")
		}
		c.receiveLimit = bytes
		return nil
	}
}

// WithMetadataLimit caps the number of columns accepted in a result. The default
// is 65535, the protocol maximum. This bound is checked before allocating a peer-sized column slice.
func WithMetadataLimit(columns int) Option {
	return func(c *Config) error {
		if columns < 1 || columns > 65535 {
			return fmt.Errorf("mariadb: column limit must be in 1..65535")
		}
		c.columnLimit = columns
		return nil
	}
}

// WithMetadataByteLimit bounds the combined column-definition payload bytes in
// one metadata block. The default is 16 MiB. Headers are checked against the
// remaining budget before reading bodies, including metadata that is discarded.
func WithMetadataByteLimit(bytes int) Option {
	return func(c *Config) error {
		if bytes < 1 {
			return fmt.Errorf("mariadb: metadata byte limit must be positive")
		}
		c.metadataByteLimit = bytes
		return nil
	}
}

func (mc *mysqlConn) metadataBudget() int {
	if mc.cfg != nil && mc.cfg.metadataByteLimit > 0 {
		return mc.cfg.metadataByteLimit
	}
	return 16 << 20
}

// WithAuthenticationLimit bounds each incoming authentication message before
// its body is read. The default is 1 MiB; ordinary row reads do not check an
// authentication-phase flag. This bound does not replace plugin CPU limits.
func WithAuthenticationLimit(bytes int) Option {
	return func(c *Config) error {
		if bytes < 1 {
			return fmt.Errorf("mariadb: authentication limit must be positive")
		}
		c.authLimit = bytes
		return nil
	}
}

func (mc *mysqlConn) receiveLimit() int {
	if mc.cfg != nil && mc.cfg.receiveLimit > 0 {
		return mc.cfg.receiveLimit
	}
	if mc.maxAllowedPacket > 0 {
		return mc.maxAllowedPacket
	}
	return defaultMaxAllowedPacket
}

func (mc *mysqlConn) columnLimit() int {
	if mc.cfg != nil && mc.cfg.columnLimit > 0 {
		return mc.cfg.columnLimit
	}
	return 65535
}

func (mc *mysqlConn) authLimit() int {
	if mc.cfg != nil && mc.cfg.authLimit > 0 {
		return mc.cfg.authLimit
	}
	return 1 << 20
}

// protocolError retires a stream whose next command boundary is unknown. Return
// a protocol error, never ErrBadConn: database/sql must not replay a write that
// may already have executed. The nil channel case supports decoder-only uses.
func (mc *mysqlConn) protocolError() error {
	if mc.closech != nil {
		mc.close()
	} else {
		mc.closed.Store(true)
	}
	return ErrMalformPkt
}
