// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

// terminatorStatus decodes either the legacy EOF or its negotiated OK
// replacement. Read and discard paths must validate the same bytes: cleanup
// is still processing untrusted protocol input. This does not update command
// counters, since a result terminator is not another executed statement.
func (mc *mysqlConn) terminatorStatus(data []byte) (statusFlag, error) {
	if len(data) < 5 || data[0] != iEOF {
		return 0, mc.protocolError()
	}
	pos := 3 // legacy EOF: marker, warnings, status
	if mc.capabilities&clientDeprecateEOF != 0 {
		pos = 1
		for range 2 { // affected_rows and last_insert_id
			_, null, n := checkedLengthEncodedInteger(data[pos:])
			if n == 0 || null {
				return 0, mc.protocolError()
			}
			pos += n
		}
		// Protocol 4.1 requires both status and warning counts.
		if len(data)-pos < 4 {
			return 0, mc.protocolError()
		}
		if mc.capabilities&clientSessionTrack != 0 {
			if err := mc.readSessionTail(data[pos+4:], readStatus(data[pos:])); err != nil {
				return 0, err
			}
		}
	} else if mc.capabilities&clientSessionTrack != 0 && mc.session != nil {
		mc.session.Changes = nil // Legacy EOF has no tracking payload.
	}
	return readStatus(data[pos:]), nil
}

// malformedRow invalidates borrowed values and retires a structurally invalid
// stream. Preserve the decoder's error; never ask database/sql to replay a
// command whose response may already reflect executed writes.
func (rows *mysqlRows) malformedRow(err error) error {
	rows.rawCols = nil
	rows.mc.protocolError()
	return err
}
