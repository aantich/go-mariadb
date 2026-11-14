// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func little(b []byte) uint64 {
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}
func big(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

// Decode converts a present cell without guessing signedness, text encoding,
// timezone, geometry layout or enum labels. Integers with absent signedness
// metadata return Integer; known integers return int64/uint64. DECIMAL returns
// an exact string, temporal fields return Temporal, and strings/blobs/geometry
// return their owned bytes. Missing columns are an error; SQL NULL returns nil.
func (c Cell) Decode(column Column) (any, error) {
	if !c.Present {
		return nil, fmt.Errorf("%w: column absent from sparse image", ErrMalformed)
	}
	if c.Null {
		return nil, nil
	}
	b := c.Raw
	length := len(b)
	typ := column.Type
	width := 0
	switch typ {
	case 1:
		width = 1
	case 2:
		width = 2
	case 3:
		width = 4
	case 8:
		width = 8
	case 9:
		width = 3
	}
	if width > 0 {
		if length != width {
			return nil, ErrMalformed
		}
		v := little(b)
		if !column.SignednessKnown {
			return Integer{v, uint8(width)}, nil
		}
		if column.Unsigned {
			return v, nil
		}
		shift := uint(64 - width*8)
		return int64(v<<shift) >> shift, nil
	}
	switch typ {
	case 4:
		if length != 4 {
			return nil, ErrMalformed
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
	case 5:
		if length != 8 {
			return nil, ErrMalformed
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
	case 13:
		if length != 1 {
			return nil, ErrMalformed
		}
		if b[0] == 0 {
			return 0, nil
		}
		return 1900 + int(b[0]), nil
	case 16:
		if column.Metadata < 1 || column.Metadata > 64 || length != (int(column.Metadata)+7)/8 {
			return nil, ErrMalformed
		}
		v := big(b)
		if column.Metadata < 64 && v>>column.Metadata != 0 {
			return nil, ErrMalformed
		}
		return v, nil
	case 247, 248:
		if column.Metadata < 1 || column.Metadata > 8 || length != int(column.Metadata) || (typ == 247 && column.Metadata > 2) {
			return nil, ErrMalformed
		}
		return little(b), nil
	case 15, 253, 254, 252, 255:
		return b, nil
	case 246:
		return decodeDecimal(b, int(column.Metadata>>8), int(column.Metadata&255))
	case 7, 10, 11, 12, 17, 18, 19:
		return decodeTemporal(b, typ, int(column.Metadata))
	default:
		return nil, fmt.Errorf("%w: value type %d", ErrUnsupported, typ)
	}
}
func decodeDecimal(raw []byte, precision, scale int) (string, error) {
	if err := validateDecimal(raw, precision, scale, false); err != nil {
		return "", err
	}
	if precision < 1 || precision > 65 || scale < 0 || scale > 38 || scale > precision {
		return "", ErrMalformed
	}
	integer := precision - scale
	expected := integer/9*4 + decimalBytes[integer%9] + scale/9*4 + decimalBytes[scale%9]
	if len(raw) != expected {
		return "", ErrMalformed
	}
	b := append([]byte(nil), raw...)
	negative := b[0]&128 == 0
	b[0] ^= 128
	if negative {
		for i := range b {
			b[i] ^= 255
		}
	}
	r := reader{data: b}
	var result strings.Builder
	if negative {
		result.WriteByte('-')
	}
	group := func(digits int, padded bool) error {
		if digits == 0 {
			return nil
		}
		size := 4
		if digits < 9 {
			size = decimalBytes[digits]
		}
		v := r.be(size)
		limit := uint64(1)
		for range digits {
			limit *= 10
		}
		if v >= limit {
			return ErrMalformed
		}
		text := strconv.FormatUint(v, 10)
		if padded {
			for range digits - len(text) {
				result.WriteByte('0')
			}
		}
		result.WriteString(text)
		return r.err
	}
	first := true
	if integer%9 != 0 {
		if err := group(integer%9, false); err != nil {
			return "", err
		}
		first = false
	}
	for range integer / 9 {
		if err := group(9, !first); err != nil {
			return "", err
		}
		first = false
	}
	if first {
		result.WriteByte('0')
	}
	if scale > 0 {
		result.WriteByte('.')
		for range scale / 9 {
			if err := group(9, true); err != nil {
				return "", err
			}
		}
		if err := group(scale%9, true); err != nil {
			return "", err
		}
	}
	text := result.String()
	sign := ""
	if negative {
		sign = "-"
		text = text[1:]
	}
	whole, fraction, hasFraction := strings.Cut(text, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	text = sign + whole
	if hasFraction {
		text += "." + fraction
	}
	return text, r.end()
}

func validateDecimal(raw []byte, precision, scale int, unsigned bool) error {
	if precision < 1 || precision > 65 || scale < 0 || scale > 38 || scale > precision {
		return ErrMalformed
	}
	integer := precision - scale
	expected := integer/9*4 + decimalBytes[integer%9] + scale/9*4 + decimalBytes[scale%9]
	if len(raw) != expected {
		return ErrMalformed
	}
	negative := raw[0]&128 == 0
	if unsigned && negative {
		return ErrMalformed
	}
	index := 0
	group := func(digits int) bool {
		if digits == 0 {
			return true
		}
		size := 4
		if digits < 9 {
			size = decimalBytes[digits]
		}
		value := uint64(0)
		for range size {
			b := raw[index]
			if index == 0 {
				b ^= 128
			}
			if negative {
				b ^= 255
			}
			value = value<<8 | uint64(b)
			index++
		}
		limit := uint64(1)
		for range digits {
			limit *= 10
		}
		return value < limit
	}
	if !group(integer % 9) {
		return ErrMalformed
	}
	for range integer / 9 {
		if !group(9) {
			return ErrMalformed
		}
	}
	for range scale / 9 {
		if !group(9) {
			return ErrMalformed
		}
	}
	if !group(scale % 9) {
		return ErrMalformed
	}
	return nil
}
func decodeTemporal(raw []byte, typ byte, precision int) (Temporal, error) {
	t := Temporal{Type: typ}
	base := 0
	switch typ {
	case 7:
		base = 4
		precision = 0
	case 10, 11:
		base = 3
		precision = 0
	case 12:
		base = 8
		precision = 0
	case 17:
		base = 4
	case 18:
		base = 5
	case 19:
		base = 3
	}
	if precision < 0 || precision > 6 || len(raw) != base+(precision+1)/2 {
		return t, ErrMalformed
	}
	fraction := int64(0)
	if precision > 0 {
		b := raw[base:]
		u := big(b)
		shift := uint(64 - len(b)*8)
		fraction = int64(u<<shift) >> shift
		switch len(b) {
		case 1:
			fraction *= 10000
		case 2:
			fraction *= 100
		}
	}
	if typ != 19 && fraction < 0 {
		return t, ErrMalformed
	}
	switch typ {
	case 7:
		t.UnixSeconds = uint32(little(raw))
	case 17:
		t.UnixSeconds = uint32(big(raw[:4]))
	case 10:
		v := little(raw)
		t.Day = int(v & 31)
		t.Month = int(v >> 5 & 15)
		t.Year = int(v >> 9)
	case 12:
		v := little(raw)
		t.Second = int(v % 100)
		v /= 100
		t.Minute = int(v % 100)
		v /= 100
		t.Hour = int(v % 100)
		v /= 100
		t.Day = int(v % 100)
		v /= 100
		t.Month = int(v % 100)
		t.Year = int(v / 100)
	case 18:
		v := big(raw[:5])
		if v&0x8000000000 == 0 {
			return t, ErrMalformed
		}
		ym := (v >> 22) & 0x1ffff
		t.Year = int(ym / 13)
		t.Month = int(ym % 13)
		t.Day = int(v >> 17 & 31)
		t.Hour = int(v >> 12 & 31)
		t.Minute = int(v >> 6 & 63)
		t.Second = int(v & 63)
	case 11:
		v := int64(little(raw)<<40) >> 40
		t.Negative = v < 0
		if v < 0 {
			v = -v
		}
		t.Hour = int(v / 10000)
		t.Minute = int(v / 100 % 100)
		t.Second = int(v % 100)
	case 19:
		v := int64(big(raw[:3])) - 0x800000
		t.Negative = v < 0
		if t.Negative {
			if fraction > 0 {
				return t, ErrMalformed
			}
			if fraction != 0 {
				v++
			}
			v = -v
			fraction = -fraction
		} else if fraction < 0 {
			return t, ErrMalformed
		}
		// Do not mask reserved high bits into a plausible hour. The range
		// check below must reject them rather than exposing a different TIME.
		t.Hour = int(v >> 12)
		t.Minute = int(v >> 6 & 63)
		t.Second = int(v & 63)
	}
	t.Microsecond = int(fraction)
	quantum := 1
	for range 6 - precision {
		quantum *= 10
	}
	maxHour := 23
	if typ == 11 || typ == 19 {
		maxHour = 838
	}
	if t.Year > 9999 || t.Month > 12 || t.Day > 31 || t.Hour > maxHour || t.Minute > 59 || t.Second > 59 || t.Microsecond < 0 || t.Microsecond > 999999 || t.Microsecond%quantum != 0 {
		return t, ErrMalformed
	}
	return t, nil
}
