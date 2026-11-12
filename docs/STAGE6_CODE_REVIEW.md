# Stage 6 review: six correctness/validation findings and one performance issue

Reviewed 12 November 2026, on `master`, parent `b3300c2`. Executable source stays
at `f1ae6b97668f1fd10f61af0b1483649db49093cfec67713ce8fa9fef92e5370b`.
This is a review record; none of the findings below is fixed by this pass.
Production code and permanent tests are unchanged. All work remains uncommitted.

The review covers the dedicated replication transport, GTID types, event/table/
row/value decoders, stream lifetime and checkpoints, public examples/comments,
CI integration, fixture ownership and publication validators. Checked-out
MariaDB source supplied protocol context; the pinned setup action and MariaDB
12.3.3 packaging sources were also inspected for the CI finding.

## 1. P1: the mandatory CI fixture cannot find the bundled initializer

Location: [cdc_fixture.py:47](../validation/cdc_fixture.py#L47), invoked by the
[feature job](../.github/workflows/test.yml#L169).

The fixture resolves `mariadb-install-db` only through `PATH`. The pinned
`actions-setup-mysql@v1.56.2` exports only the selected installation's `bin`
directory. Its standalone MariaDB distribution installs the initializer and
`mysql_install_db` alias in `scripts`. The action itself starts its main server
by resolving that script explicitly. The new CDC wrapper therefore rejects the
same distribution before any correctness tests run. An unrelated system
initializer, if present, would also be an unsafe version-selection assumption.

Primary sources: [action PATH setup](https://github.com/shogo82148/actions-setup-mysql/blob/v1.56.2/src/installer.ts#L71),
[action initializer resolution](https://github.com/shogo82148/actions-setup-mysql/blob/v1.56.2/src/starter.ts#L122),
[release-tagged distribution build](https://github.com/shogo82148/build-mysql/blob/mariadb-12.3.3-20260910082800/build-mariadb-linux.sh#L184),
[MariaDB 12.3.3 standalone layout](https://github.com/MariaDB/server/blob/mariadb-12.3.3/cmake/install_layout.cmake#L111)
and [initializer installation](https://github.com/MariaDB/server/blob/mariadb-12.3.3/scripts/CMakeLists.txt#L220).
Copies of these sources are saved with the review evidence.

**Evidence:** The layout probe creates the same `bin`/`scripts` arrangement and
uses the real fixture entry point. It fails with “MariaDB server/client binaries
are required” before initialization. This is a packaging-source/layout
reproduction, not a completed remote GitHub Actions run. The previous green Linux
receipt used distribution-installed binaries, so it does not cover this layout.

**Correction:** Resolve the initializer from the selected server installation,
including standalone and system-package layouts, and pass its matching basedir.
Make the feature workflow provision/select that complete toolchain explicitly.
Test the actual standalone layout without depending on unrelated host binaries.

## 2. P2: a supported SQL cursor setting prevents CDC from opening

Location: [replication.go:147](../replication.go#L147).

`replicationScalar` calls `mc.query` for the setup/version probes. That SQL fast
path deliberately returns `driver.ErrSkip` when `WithServerCursors` is enabled,
so `database/sql` can prepare the query instead. Replication setup is outside
`database/sql` and has no such fallback. Reusing a valid cursor-enabled Config
therefore makes `cdc.Open` fail before sending COM_BINLOG_DUMP.

**Evidence:** A race-enabled probe against the private real MariaDB fixture calls
`cfg.Apply(mariadb.WithServerCursors(8))`, then `cdc.Open`. It fails with
`driver: skip fast-path; continue as if unimplemented`. This is a supported
configuration interaction, not a malformed-server case.

**Correction:** Keep SQL cursor routing out of replication's internal scalar
queries, without mutating the caller's Config. Add a live regression reusing
cursor-enabled configuration and verify the original SQL cursor policy survives.

## 3. P2: completed file checkpoints can move backwards

Location: [decoder.go:104](../cdc/decoder.go#L104).

Completion checks only that the file is nonempty and the position is at least
four. It does not check position against the requested start or earlier complete
progress in the same file. The increasing private token sequence therefore does
not establish increasing file progress. A stale or malformed but CRC-valid
header can publish and acknowledge a checkpoint earlier than the last completed
group, making a persisted file Resume point backwards.

**Evidence:** A file-mode stream starting at offset 1000 accepts one complete
group at `bin.000001:1083`, followed by another at `bin.000001:900`. The isolated
race-enabled probe fails its assertion that completed progress cannot regress.
This is a protocol-fault counterexample; ordinary healthy-server streams were
not observed regressing during the live suite.

**Correction:** Validate file-coordinate progress while handling artificial
bootstrap events and rotations separately. Add start-boundary, backwards-offset
and 32-bit rollover cases. Unsupported rollover should fail explicitly before
issuing an invalid resume/semisync coordinate.

## 4. P2: malformed EOF packets are accepted as clean completion

Location: [replication.go:244](../replication.go#L244).

Every packet starting with `0xfe` and shorter than nine bytes becomes `io.EOF`,
without validating its terminator fields or negotiated variant. A two-byte
payload `fe 00` is therefore accepted even though it is not a complete
terminator. At a complete-group boundary, Stream treats it as a clean source
end, and the documented consume loop can return success instead of reporting a
protocol failure.

**Evidence:** The race-enabled transport probe reproduces `io.EOF` from `fe 00`.
An empty invalid zlib frame, tested as a control, is correctly rejected; that
control is not an additional finding. Ordinary socket truncation already goes
through the existing buffer's unexpected-EOF handling.

**Correction:** Validate the complete supported EOF/OK terminator forms for the
negotiated protocol before returning `io.EOF`. Preserve errors for malformed
lengths, fields and compression failures; cover both checksum/event boundaries
and an incomplete group.

## 5. P2: fixture cleanup skips surviving descendants after leader exit

Location: [cdc_fixture.py:23](../validation/cdc_fixture.py#L23).

`stop(..., group=True)` does nothing when `process.poll()` says the group leader
has exited. It also returns once the leader exits after SIGTERM. Neither proves
that its process group is gone. A correctness child can exit on an exception
while leaving a Go test binary or other descendant alive. The fixture then
removes its datadir and stops its server while that worker remains running.

**Evidence:** A review-only process starts in a new session, launches a sleeping
child in that group, and exits. The real `stop` function leaves the child alive.
The probe's own `finally` kills that explicitly owned group; no unrelated PID or
process group is signalled.

**Correction:** Track and terminate the owned process group independently of
leader liveness, with bounded grace and forced cleanup for remaining descendants.
Ensure server cleanup still runs if child cleanup raises. Test an already-exited
leader and a leader that exits while a descendant ignores termination.

## 6. P2: the decoder fuzzer bypasses important payload parsers

Location: [decoder_test.go:564](../cdc/decoder_test.go#L564).

Each fuzz input gets an already-active GTID and an empty table map. Consequently
every GTID input exits as a nested GTID before parsing its fields. Every row
input exits at the table-ID lookup before decoding column counts, sparse/null
bitmaps or packed values. A table-map input cannot prime the next input because
the decoder is recreated each time. Scalar Decode fuzzing does not cover those
row-framing paths.

**Evidence:** A probe copies the exact fuzz setup and submits its valid GTID and
row seeds. They are rejected as “nested GTID” and “stale or unknown table ID”.
The recorded fuzz executions are real, but their count does not establish
coverage of these excluded payload parsers.

**Correction:** Use valid per-family starting states, including a matching table
map for rows and an idle state for GTIDs. Add valid compressed/fragmented seeds
and a bounded sequence fuzzer for group/schema/rotation transitions. Keep the
invalid-state cases too, but ensure intended positive seeds reach their parsers.

## 7. P2 performance: compressed events allocate a second expanded payload

Location: [decoder.go:447](../cdc/decoder.go#L447).

The advertised uncompressed size is already bounded, but expansion first uses
`io.ReadAll` to grow one buffer, then allocates `result` and copies the entire
expanded payload into it. Two full expanded buffers are live at that point,
with additional growth/copy work in ReadAll. This affects every compressed query
or row event and increases memory pressure for large compressed BLOB changes.

This is a structural allocation/copy finding from the code. No throughput,
allocation profile or timing measurement was run or inferred.

**Correction:** Allocate the final prefix-plus-expanded buffer once, inflate
directly into its payload region, and verify exact output length, zlib completion
and absence of trailing bytes. Retain all size/CRC/malformed controls. Measure
the improvement only in a later authorized benchmark window.

## Checks and evidence

* Existing focused GTID/replication/CDC tests pass under race detection.
* All 82 existing Python harness tests pass.
* `validation/verify.py` passes; original Stage 6 executable inputs and all 100
  saved artifact hashes still match. Historical timing and baseline bytes are
  unchanged.
* New review probes are isolated Go overlays or a separate Python script stored
  as `.txt` evidence. Their assertion failures reproduce the findings above;
  no compile error is counted as a counterexample.
* The shared MariaDB service is unchanged. The live Config probe uses its own
  socket-only temporary MariaDB; its wrapper cleans up after the failed test.
* No full Mac/Linux-suite rerun, benchmark, benchmark smoke, profiling workload,
  implementation fix, commit or push was performed for this review.

[Review manifest](../validation/results/stage6-review-20261112/capture.json) ·
[Evidence directory](../validation/results/stage6-review-20261112).

Fix the CI bootstrap and Config interaction first, then coordinate/terminator
validation and cleanup. Expand fuzz reachability and remove the redundant
expansion buffer in the same correction pass, before repeating final correctness
checks. Keep existing successful receipts as historical coverage; these newly
reproduced cases remain pending.
