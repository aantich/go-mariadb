# Stage 4 measurement and read-path investigation

**Later functional-stage follow-up:** Stage 5 adds authentication/provider inputs
after this preparation receipt. Its saved fingerprint remains historical. Re-run
the complete profile/source preflight before future authorized measurements and
use the external-auth fixture wrapper described in
[Stage 5](STAGE5_AUTHENTICATION.md). No Stage 4 reference has been frozen.

12 November 2026. **Prepared, not executed.** Anton explicitly selected
"Continue preparation only; no benchmarks yet". This runbook does not authorize
its timing or profiling commands. Stage 4 and its review corrections are
published at `527b5b9577dedfce03ee1fa70ea22a7ed1f94a80` on `master`.

The first objective is to measure the code already implemented, establish the
62 new profiles' first reference, and identify the cost of the remaining
text-read signals. Production optimizations follow evidence from that pass.
No production or executable workload inputs change during this preparation.

## Readiness and preserved results

The [read-only preflight receipt](../validation/results/stage4-measurement-preparation-20261112/readiness.json)
records the exact commit/source fingerprint, compiler, hardware, resolved module
graph, server settings and existing reference hashes. Checks require the original
workloads to match their frozen bodies after only the approved identity/contract
adapters. Private configuration stays in the ignored owner-only file.

The current executable-input fingerprint is
`d48b450072dd1b8e7405efdaa5888e01c18849b6e40d8ec927fbb62e41603fc7`.
Existing correctness evidence records 1,304 Mac / 1,307 Linux passing leaves,
zero failures, race/vet and 69 harness checks. These are earlier results, not
tests repeated during this preparation. The current timing capture still
contains 200 historical profiles, with SHA-256
`02fea12c04c5c42f1596bdb0c56ddd3839afb26a86f7e3e61f0b7e798adea7ed`.

Readiness is a point-in-time check. Immediately before execution, reconfirm the
host conditions and matching hardware/compiler/modules/server. Changed profiles
need separate references. Do not change shared-server globals or other projects'
schemas to obtain a match. The Mac performance_schema setting stays OFF; isolated
Linux instrumentation coverage is separate from Mac timing measurements.

## 1. Measure the complete committed implementation

After explicit authorization, execute from the Go checkout. The condition text
must describe the actual window; do not copy an old idle-host statement.

```sh
python3 validation/record.py --freeze-stage4-baseline \
  --conditions 'Replace with the actual confirmed and observed host conditions'
python3 validation/verify.py
```

The collector runs ordinary correctness, the complete race suite, vet, all
harness checks and both fuzz campaigns before building its benchmark binaries.
Failed checks prevent timing execution. It then measures all **262 profiles in
seven fresh processes per family: 1,834 samples**, using the existing seeded
family order, GOMAXPROCS=4 and 200 ms calibration. Allow a sustained measurement
window; this expanded suite is not a three-minute smoke check.

Use the first complete attempt as the first new reference, even when its results
are disappointing. Retain incomplete/failed attempts as well. The collector
records source stability, before/after server provenance, raw samples and
per-family host observations. It protects each earlier matching correctness
pass independently of the historical timing capture. CPU snapshots qualify the
conditions; they cannot prove the cause of an individual timing change.

Only a complete, successful, matching capture may create
`validation/baselines/native-stage4-macos-arm64-mariadb-12.3.3-go1.27.4/`.
An existing directory must never be overwritten. Later captures omit
`--freeze-stage4-baseline`. The original upstream and native-bulk references stay
immutable. The collector retains the prior timing capture and publishes the
new full evidence and generated live report only after successful validation.

Do not rerun just to find better numbers. If an identified fault or a changed
measurement condition warrants confirmation, declare the complete confirmation
before measuring and preserve both captures. Report the chosen complete run,
including all its slower profiles; do not select favorable samples per profile.

## 2. Compare the right alternatives

Keep Baseline / Current / Change columns in the live report. For the original
184 profiles, Baseline is unchanged upstream. The sixteen earlier native-bulk
profiles retain their independent first reference. The 62 new Stage 4 profiles
use their own first measurement, so their initial zero change is not evidence
of improvement over upstream. Keep the three reference scopes explicit.

Supplement that longitudinal comparison with alternatives from the same run:

| Question | Matched comparison | Interpretation |
| --- | --- | --- |
| Does native handle reuse help? | Aggregate once/reused and units once/reused at 1/16/100/1,000 rows | Ratio of median ns/op, with allocations and the prepared loop beside it |
| Does streaming help? | Reader once/reused, native bytes reused, ordinary bytes once and sql.Stmt at each payload size | Full transaction, payload generation and digest verification; B/op is total allocation, not peak retained heap |
| What is the cursor tradeoff? | Fetch 1/16/128/1,024 versus ordinary prepared fetch 0, with identical row counts and scan ownership | Local round trips and allocations; no WAN or server-memory claim |
| What does progress observation cost? | Disabled, negotiated/discarded, callback and callback-error DDL; quiet-command controls separately | DDL results require actual progress reports; callback failure does not cancel SQL |

Preserve workload units: rows/transaction, payload bytes/transaction, complete
row-set consumption, two ALTER commands plus verification, or one quiet command.
Retained setup/preparation is outside timing; one-shot preparation remains timed.
Do not compare unlike work or present these API-alternative ratios as a universal
fork-versus-upstream speedup. Examine all seven samples, not only their medians.
Repeated >5% elapsed-time or allocation changes are investigation signals;
isolated noisy samples do not justify weakening correctness or advancing a reference.

## 3. Attribute text reads separately from accepted timings

Keep diagnostics out of the full timing window. The existing profiler can first
show its exact commands without executing anything:

```sh
python3 validation/profile_reads.py --case text-narrow-owned \
  --output validation/.local/profiles/stage4-text-narrow-owned
```

After profiling authorization, its `--execute --conditions 'Actual conditions'`
mode retains a unique diagnostic capture, binary, CPU/heap profiles, logs and
endpoint/source checksums. Repeat for `text-blob-owned` and
`text-blob-borrowed`, using a new directory for every attempt. Never publish
instrumented ns/op as a benchmark comparison or let diagnostics update references.

The existing tool collects CPU and allocation profiles together with
`memprofilerate=1`. That is useful for allocation attribution, but recording
every allocation perturbs CPU costs. Before attributing a CPU bottleneck,
collect a separate CPU-only run of the same case, with allocation profiling
disabled, and retain equivalent command/source/server provenance. That separate
mode or recorded invocation remains preparation work for the authorized
diagnostic pass; the combined CPU view alone is insufficient evidence.

Inspect application validation, database/sql conversion, driver decoding,
packet copies, allocator/GC work and socket waits separately. The SQL workload
includes server work and content checks. A client CPU profile cannot establish
a server bottleneck; low client CPU is not proof of network latency. If a
fork-versus-upstream cause remains plausible, collect a matched original-driver
control from immutable source in an isolated export, with identical workload
bodies, compiler, dependencies and endpoint. Do not switch this working branch
or overwrite the historical baseline to obtain that control.

Owned []byte/string values and borrowed sql.RawBytes have different lifetimes.
Preserve required ownership copies, checked conversion, malformed-input bounds,
Rows cleanup, cancellation and no-replay guarantees. Borrowed BLOB reads also
had an earlier slowdown, so eliminating ownership copies is not an established
solution. Make one measured candidate change at a time, add meaningful
correctness coverage, then compare matched complete captures before accepting it.

## Deliverables and later work

The later measurement pass should produce the complete capture/reference,
generated live report, a reviewable summary of reuse/stream/cursor/progress
results, separate diagnostic evidence, and explicit unresolved signals. Any
production optimization needs its own explanation and validation. Commit/push
and package release are separate from this preparation request.

Evaluate pipelining after measured reuse/read-path results. Interactive
PAM/GSSAPI fixtures, optional CDC, wider server/compiler/platform coverage and
real application pilots remain later work; readiness remains Alpha.
