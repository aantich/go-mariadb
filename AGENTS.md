# Current authorization, 11 November 2026

Publication authorization, 12 November 2026: Anton requested committing and
pushing the completed Go driver work to this fork's existing `master` branch.
This supersedes the uncommitted-only status for this delivery. It does not
authorize a package release, upstream submissions or new performance runs.

Validation restriction, 12 November 2026: Anton is heavily using the Mac and
requested correctness checks only. Do not run further performance benchmarks,
including benchmark smoke checks, during this pass. Preserve completed captures
and their contention qualifications; do not use them as performance acceptance.

Anton chose a dedicated MariaDB driver and authorized implementation. Use module
`github.com/aantich/go-mariadb`, package `mariadb` and SQL registration `mariadb`.
The earlier upstream-first recommendation was declined; do not send PRs or other
messages upstream. Keep this work uncommitted until requested. No package release
or publication is authorized. Historical baseline instructions follow; the
source-unchanged/rename restriction described the initial review only.

Use `validation/record.py` for current captures and `validation/verify.py` for
freshness. Original `run.py` and `check.py` remain frozen historical workloads.
The exact inverse identity adapter must reproduce original workload hashes.

# MariaDB Go driver work

User authorization, 11 November 2026: inspect the newly cloned Go fork, compare
with MariaDB, run relevant tests, create a proper correctness/benchmark suite,
record an unchanged-driver baseline and propose implementation stages. The
current pass does not authorize package publication or a driver/module rename.

Read `docs/MARIADB_REVIEW.md`, `docs/ROADMAP.md` and `validation/README.md` before
implementation. Work in the existing checkout, inspect/preserve changes first.
The inherited branch is `master`; the persistent `main` instruction in the
parent project specifically names the Haskell driver. Do not silently rename
this branch or module as part of a benchmark or correctness fix.

Keep the original driver at `e6232456bb6eb92a117563823ac6956937410b96` identifiable.
The first baseline under `validation/baselines/` is immutable, including failed
assertions, raw logs and benchmark workloads. Add new feature tests/benchmarks
in separate files. Fix actual driver code rather than changing assertions to
obtain green results. Label a deliberately new API contract as a gap.

Run/provision a real MariaDB before integration or performance measurement.
Use only task-owned Go schemas/accounts, keep credentials in ignored owner-only
files and never change shared-server global settings or application data for
test convenience. Missing fixture coverage is explicit, not a passing test.

Use `validation/run.py` for complete source-fingerprinted captures and
`validation/check.py` for freshness/baseline verification. Preserve Baseline,
Current, Change vs baseline columns and precise units. New machine/compiler/
server/network configurations require separate profiles; Go/Haskell samples
are not interchangeable speed comparisons. Documentation-only edits do not
justify repeating a complete benchmark capture.

The target is MariaDB only. Preserve protocol names MariaDB uses, including
mysql_native_password and shared wire identifiers. Do not claim inherited
metadata caching, ed25519 or Go 1.27 scanning as new fork improvements.

Write idiomatic Go and helpful public API/resource/error/lifetime comments.
Preserve database/sql ownership, cancellation and retry contracts. Returning
ErrBadConn after a potentially executed write can cause unsafe automatic replay.
Do not silently turn liveness-only pool checkout into a destructive reset.

Leave this initial review/test/plan pass uncommitted for review; no commit,
push or public Go release was requested.
