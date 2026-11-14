// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"bytes"
	"fmt"
	"math"
	"math/bits"
)

func cloneTable(t Table) Table {
	t.Columns = append([]Column(nil), t.Columns...)
	metadata := make([]Metadata, len(t.Metadata))
	for i, m := range t.Metadata {
		metadata[i] = Metadata{m.Type, bytes.Clone(m.Data)}
	}
	t.Metadata = metadata
	return t
}
func numeric(typ byte) bool {
	switch typ {
	case 1, 2, 3, 4, 5, 8, 9, 13, 246:
		return true
	}
	return false
}
func (d *decoder) tableMap(body []byte) (Table, int, error) {
	r := reader{data: body}
	t := Table{}
	h := d.postHeader(TableMapEvent)
	if h != 6 && h != 8 {
		return t, 0, fmt.Errorf("%w: table-map post-header", ErrUnsupported)
	}
	width := 6
	if h == 6 {
		width = 4
	}
	t.ID = r.le(width)
	t.Flags = uint16(r.le(2))
	t.Schema = string(r.take(int(r.byte())))
	if r.byte() != 0 {
		return t, 0, ErrMalformed
	}
	t.Name = string(r.take(int(r.byte())))
	if r.byte() != 0 {
		return t, 0, ErrMalformed
	}
	count := r.count(d.limits.MaxColumns)
	if r.err != nil {
		return t, 0, r.err
	}
	if count == 0 {
		return t, 0, ErrMalformed
	}
	types := r.take(count)
	metaLen := r.count(2 * count)
	meta := reader{data: r.take(metaLen)}
	if r.err != nil {
		return t, 0, r.err
	}
	t.Columns = make([]Column, count)
	for i, typ := range types {
		c := Column{Type: typ, WireType: typ}
		switch typ {
		case 1, 2, 3, 6, 7, 8, 9, 10, 11, 12, 13:
		case 4, 5:
			c.Metadata = uint16(meta.byte())
			want := uint16(4)
			if typ == 5 {
				want = 8
			}
			if c.Metadata != want {
				return t, 0, ErrMalformed
			}
		case 15, 253:
			c.Metadata = uint16(meta.le(2))
			if c.Metadata == 0 {
				return t, 0, ErrMalformed
			}
		case 16:
			low, high := meta.byte(), meta.byte()
			if low > 7 || int(high)*8+int(low) < 1 || int(high)*8+int(low) > 64 {
				return t, 0, ErrMalformed
			}
			c.Metadata = uint16(high)*8 + uint16(low)
		case 17, 18, 19:
			c.Metadata = uint16(meta.byte())
			if c.Metadata > 6 {
				return t, 0, ErrMalformed
			}
		case 246:
			precision, scale := meta.byte(), meta.byte()
			if precision == 0 || precision > 65 || scale > 38 || scale > precision {
				return t, 0, ErrMalformed
			}
			c.Metadata = uint16(precision)<<8 | uint16(scale)
		case 252, 255:
			c.Metadata = uint16(meta.byte())
			if c.Metadata < 1 || c.Metadata > 4 {
				return t, 0, ErrMalformed
			}
		case 254:
			hi, lo := meta.byte(), meta.byte()
			if hi == 247 || hi == 248 {
				c.Type = hi
				c.Metadata = uint16(lo)
				if (hi == 247 && (lo < 1 || lo > 2)) || (hi == 248 && (lo < 1 || lo > 8)) {
					return t, 0, ErrMalformed
				}
			} else if hi == 0 {
				c.Metadata = uint16(lo)
			} else if hi&0x30 != 0x30 && hi|0x30 == 254 {
				c.Metadata = uint16(hi&0x30^0x30)<<4 | uint16(lo)
			} else if hi == 254 {
				c.Metadata = uint16(lo)
			} else {
				return t, 0, fmt.Errorf("%w: STRING subtype", ErrUnsupported)
			}
		default:
			return t, 0, fmt.Errorf("%w: column type %d", ErrUnsupported, typ)
		}
		t.Columns[i] = c
	}
	if err := meta.end(); err != nil {
		return t, 0, err
	}
	nulls := r.take((count + 7) / 8)
	if r.err != nil {
		return t, 0, r.err
	}
	for i := range t.Columns {
		t.Columns[i].Nullable = bit(nulls, i)
	}
	seen := make(map[byte]bool)
	for r.pos < len(body) && r.err == nil {
		if len(t.Metadata) >= 256 {
			return t, 0, ErrLimit
		}
		kind := r.byte()
		n := r.count(len(body))
		value := r.take(n)
		if r.err != nil {
			return t, 0, r.err
		}
		if seen[kind] {
			return t, 0, fmt.Errorf("%w: duplicate optional metadata", ErrMalformed)
		}
		seen[kind] = true
		copy := bytes.Clone(value)
		t.Metadata = append(t.Metadata, Metadata{kind, copy})
		switch kind {
		case 1:
			numericCount := 0
			for _, c := range t.Columns {
				if numeric(c.Type) {
					numericCount++
				}
			}
			if len(value) != (numericCount+7)/8 {
				return t, 0, fmt.Errorf("%w: signedness bitmap", ErrMalformed)
			}
			index := 0
			for i := range t.Columns {
				c := &t.Columns[i]
				if numeric(c.Type) {
					c.SignednessKnown = true
					c.Unsigned = value[index/8]&(0x80>>uint(index%8)) != 0
					index++
				}
			}
		case 4:
			names := reader{data: value}
			for i := range t.Columns {
				length := names.count(len(value))
				t.Columns[i].Name = string(names.take(length))
			}
			if err := names.end(); err != nil {
				return t, 0, err
			}
		}
	}
	if err := r.end(); err != nil {
		return t, 0, err
	}
	charge, err := tableCharge(len(body), count, len(t.Metadata))
	return t, charge, err
}

// Account for retained bytes, strings, slices and map overhead conservatively.
// Columns/TLV counts are already bounded; the wire term can approach MaxInt
// on 32-bit builds and must be checked before multiplying it.
func tableCharge(wireBytes, columns, fields int) (int, error) {
	overhead := 512 + columns*96 + fields*32
	if wireBytes > (math.MaxInt-overhead)/2 {
		return 0, ErrLimit
	}
	return 2*wireBytes + overhead, nil
}
func (d *decoder) rows(typ byte, body []byte) (Rows, error) {
	r := reader{data: body}
	result := Rows{}
	h := d.postHeader(typ)
	if (typ >= WriteRowsV2 && h != 10) || (typ < WriteRowsV2 && h != 6 && h != 8) {
		return result, fmt.Errorf("%w: rows post-header", ErrUnsupported)
	}
	width := 6
	if h == 6 {
		width = 4
	}
	id := r.le(width)
	result.Flags = uint16(r.le(2))
	if typ >= 30 && typ <= 32 {
		n := int(r.le(2))
		if n < 2 {
			return result, ErrMalformed
		}
		r.take(n - 2)
	}
	entry, ok := d.tables[id]
	if !ok {
		return result, fmt.Errorf("%w: stale or unknown table ID", ErrMalformed)
	}
	count := r.count(d.limits.MaxColumns)
	if r.err != nil {
		return result, r.err
	}
	if count != len(entry.table.Columns) {
		return result, fmt.Errorf("%w: table column count changed", ErrMalformed)
	}
	before := r.take((count + 7) / 8)
	after := before
	update := typ == UpdateRowsV1 || typ == UpdateRowsV2
	if update {
		after = r.take((count + 7) / 8)
	}
	if r.err != nil {
		return result, r.err
	}
	// Charge all cells, including absent columns, before constructing each image.
	// Otherwise a tiny sparse image could allocate an enormous column matrix.
	cells := 0
	image := func(present []byte) ([]Cell, error) {
		if count > d.limits.MaxCells-cells {
			return nil, ErrLimit
		}
		cells += count
		n := 0
		for _, b := range present {
			n += bits.OnesCount8(b)
		}
		if count%8 != 0 && present[len(present)-1]>>uint(count%8) != 0 {
			return nil, ErrMalformed
		}
		if n == 0 {
			return nil, fmt.Errorf("%w: empty row image", ErrMalformed)
		}
		nulls := r.take((n + 7) / 8)
		if r.err != nil {
			return nil, r.err
		}
		values := make([]Cell, count)
		index := 0
		for i, c := range entry.table.Columns {
			if !bit(present, i) {
				continue
			}
			values[i].Present = true
			values[i].Null = bit(nulls, index) || c.Type == 6
			index++
			if values[i].Null {
				if !c.Nullable && c.Type != 6 {
					return nil, fmt.Errorf("%w: NULL in nonnullable column", ErrMalformed)
				}
				continue
			}
			raw, err := readValue(&r, c)
			if err != nil {
				return nil, err
			}
			values[i].Raw = raw
		}
		return values, r.err
	}
	for r.pos < len(body) && r.err == nil {
		start := r.pos
		first, err := image(before)
		if err != nil {
			return result, err
		}
		change := Change{}
		if update {
			change.Before = first
			change.After, err = image(after)
			if err != nil {
				return result, err
			}
		} else if typ == DeleteRowsV1 || typ == DeleteRowsV2 {
			change.Before = first
		} else {
			change.After = first
		}
		if r.pos <= start {
			return result, ErrMalformed
		}
		result.Changes = append(result.Changes, change)
	}
	if len(result.Changes) == 0 {
		return result, ErrMalformed
	}
	result.Table = cloneTable(entry.table)
	return result, r.end()
}

var decimalBytes = [10]int{0, 1, 1, 2, 2, 3, 3, 4, 4, 4}

func readValue(r *reader, c Column) ([]byte, error) {
	n := 0
	switch c.Type {
	case 1, 13:
		n = 1
	case 2:
		n = 2
	case 3, 4, 7:
		n = 4
	case 8, 5, 12:
		n = 8
	case 9, 10, 11:
		n = 3
	case 17:
		n = 4 + (int(c.Metadata)+1)/2
	case 18:
		n = 5 + (int(c.Metadata)+1)/2
	case 19:
		n = 3 + (int(c.Metadata)+1)/2
	case 16:
		n = (int(c.Metadata) + 7) / 8
	case 246:
		precision, scale := int(c.Metadata>>8), int(c.Metadata&255)
		integer := precision - scale
		n = integer/9*4 + decimalBytes[integer%9] + scale/9*4 + decimalBytes[scale%9]
	case 247, 248:
		n = int(c.Metadata)
	case 15, 253, 254:
		width := 1
		if c.Metadata >= 256 {
			width = 2
		}
		length := r.le(width)
		if length > uint64(c.Metadata) {
			return nil, fmt.Errorf("%w: string width", ErrMalformed)
		}
		n = int(length)
	case 252, 255:
		length := r.le(int(c.Metadata))
		if length > uint64(len(r.data)-r.pos) {
			return nil, ErrMalformed
		}
		n = int(length)
	default:
		return nil, ErrUnsupported
	}
	value := r.take(n)
	if r.err != nil {
		return nil, r.err
	}
	// Deferred conversion must not defer validation past a commit checkpoint.
	// Validate packed scalar encodings without allocating their text form.
	switch c.Type {
	case 7, 10, 11, 12, 17, 18, 19:
		if _, err := decodeTemporal(value, c.Type, int(c.Metadata)); err != nil {
			return nil, err
		}
	case 246:
		if err := validateDecimal(value, int(c.Metadata>>8), int(c.Metadata&255), c.SignednessKnown && c.Unsigned); err != nil {
			return nil, err
		}
	case 16:
		if c.Metadata < 64 && big(value)>>c.Metadata != 0 {
			return nil, ErrMalformed
		}
	}
	return value, nil
}
