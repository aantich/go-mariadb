# Stage 6 review corrections

All seven findings in the [original review](STAGE6_CODE_REVIEW.md) are fixed.
The changes remain **uncommitted on `master`**, parent `b3300c2b`. Readiness
remains **Alpha**. No benchmarks, smoke benchmarks or profiling workloads ran.

## What changed

| Finding | Correction | Permanent coverage |
| --- | --- | --- |
| CI cannot find the standalone initializer | Select one complete installation from the resolved server prefix or `GO_MARIADB_CDC_BASEDIR`; resolve its `scripts`/`bin` initializer and pass the same basedir to initialization and startup. The workflow selects the prefix after the pinned setup action. | Standalone, system, symlink, explicit-prefix and unrelated-PATH cases; real pinned standalone Linux distribution. |
| SQL cursor settings prevent CDC setup | Internal scalar probes use the ordinary text-query implementation directly. Public SQL cursor routing and the caller's Config retain their policy. | Live CDC opens/reads with `WithServerCursors(8)`, then SQL reuses that Config and verifies actual `Com_stmt_fetch` activity. |
| File checkpoints can regress | Retain requested-start, physical-position and completed-group bounds. Same-file fake rotations cannot lower them; a new file starts a new range. Reject equal/backward progress and unsupported rollover before checkpoint or ACK. | Requested start, equal/backward positions, bootstrap context, rotation, GTID diagnostic offsets, cached events and rollover. |
| Truncated EOF looks successful | Accept only MariaDB's complete five-byte binlog EOF, including when ordinary SQL negotiates deprecated EOF. Retire malformed transports. | Both capability settings, lengths 1–9, complete/unfinished groups, sticky stream errors and unchanged acknowledgement state. |
| Cleanup misses surviving descendants | Signal the owned process group independently of leader liveness, wait a bounded grace, force remaining descendants to stop and reap the leader. Always attempt server cleanup even if child cleanup raises. | Real exited-leader and SIGTERM-ignoring descendant cases; server-cleanup exception path; initializer failure/timeout diagnostics. |
| Fuzz setup bypasses payload parsers | Prime the right state for each event family: idle GTIDs, active groups and matching row maps. Keep invalid-state cases too. Add all row/compression seeds and bounded stateful sequences. | Positive seed reachability tests, full fragmented-row assembly, groups, map generations and rotation; three bounded fuzz campaigns. |
| Expansion duplicates the full payload | Allocate final prefix-plus-payload storage once and inflate directly into it. Validate advertised size, full zlib completion/Adler checksum and absence of trailing bytes. | Empty/multi-byte lengths, too-short/long output, truncated/corrupt footer, trailing/second streams and retained event ownership. |

MariaDB's `Log_event::write_header` leaves transaction/statement-cache event
positions at zero. Live testing exposed this before final validation. Such events
advance a physical byte lower bound, but **never create checkpoints**. A complete
group still needs an explicit advancing position. Counting cached bytes also
prevents a large group from hiding position rollover. Artificial connection
context does not advance that bound. Existing synthetic transcripts now generate
real forward offsets rather than deriving unrelated offsets from each event's
length; original baseline assertions and receipts remain intact.

MariaDB's `Protocol::net_send_eof` explicitly excludes `COM_BINLOG_DUMP` from the
ordinary SQL `CLIENT_DEPRECATE_EOF` conversion. Accepting an OK-shaped seven-byte
packet here would therefore be incorrect. Source excerpts and their checkout
identity are saved with the [correction evidence](../validation/results/stage6-review-fixes-20261112).

The expansion improvement removes a redundant full expanded buffer and copy by
inspection. **No measured throughput, latency or allocation improvement is
claimed.** Measurement remains queued for an authorized idle window.

## Validation

Executable fingerprint:
`69f760d553f6ec430157640a420190123790d4a636555d766126f468ab03c043`
(68 driver/native files, 103 workload/harness inputs).

| Exact-source profile | Passing leaves | Failures | Skips | Race / vet / harness |
| --- | ---: | ---: | ---: | --- |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,588 | 0 | 8 | PASS / PASS / 89 PASS |
| isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,591 | 0 | 5 | PASS / PASS / 89 PASS |

Both retain every 1,504/1,507 pre-review passing leaf, with **84 additional
passes per profile**. The 8/5 inherited/profile skips are unchanged. No required
CDC, authentication or regression check skipped; Linux instrumentation passes.

Final eight-second correctness fuzz campaigns pass **467,797 decoder**, **483,664
sequence** and **503,655 value** executions. Counts are parser exercise evidence,
not performance measurements. Positive seed assertions establish that GTID,
row/compression and fragmentation paths are reachable.

Seven isolated defect-restoration controls fail the intended permanent assertions:
cursor routing, short EOF, backward coordinates, unreachable fuzz state, omitted
expansion completion, PATH-only toolchain resolution and leader-only cleanup.
The five Go controls compile and run under race detection; no build failure is
counted as a successful control. The expansion control restores unsafe completion
validation, not a timing/allocation measurement of the original safe decoder.

CGO-disabled provider/CDC tests, Windows amd64 root/CDC cross-compilation,
Linux 386 CDC cross-compilation, actionlint and formatting/diff checks pass.
Cross-compilation does not establish Windows/386 runtime coverage. No remote
GitHub Actions run was launched; the mandatory feature job is checked locally
with the real pinned action distribution plus workflow/static policy checks.

## Evidence and remaining limits

* [Live correctness and historical timings](../validation/RESULTS.md)
* [Mac receipt](../validation/results/stage6-review-fixes-mac-final/capture.json)
* [Linux receipt](../validation/results/stage6-review-fixes-linux-final/capture.json)
* [Confirmation and hashes](../validation/results/stage6-review-fixes-confirmation.json)
* [Controls, fuzz logs and standalone fixture evidence](../validation/results/stage6-review-fixes-20261112)
* [Original implementation receipt](../validation/results/stage6-confirmation.json)
  and [original review evidence](../validation/results/stage6-review-20261112/capture.json)

Earlier successful receipts, counterexamples and failed correction attempts are
retained. Public documents that changed have byte-identical historical copies;
the new confirmation maps their old hashes to those copies. Historical timing
and immutable reference files remain unchanged. The live report publishes the
new correctness source separately from older timing samples.

Owned fixture servers, child groups and disposable containers are reaped; shared
server settings and application data are unchanged. No commit, push or release
was performed. XA/two-phase ALTER, engine-specific replication, broader MariaDB
versions/topologies, MariaDB 13.2 live partial-row tests and real application
pilots remain the [existing scope limits](STAGE6_CDC.md#scope-still-requiring-further-work).
