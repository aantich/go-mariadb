# Stage 5: interactive PAM and Kerberos GSSAPI

**Review follow-up:** All three [review findings](STAGE5_CODE_REVIEW.md) are
[corrected and validated](STAGE5_REVIEW_FIXES.md): error secrecy across switches,
native-context admission and mandatory provenance. Current matching-source
Mac/Linux correctness has **1,398/1,401 PASS**, zero failures, 77 passing harness
checks and no lost earlier passes. See the
[latest confirmation](../validation/results/stage5-review-fixes-confirmation.json).

12 November 2026. Anton requested implementing the next functional stage while
deferring benchmarks. Work continues on inherited `master`, parent `527b5b9`,
with the earlier measurement-preparation files preserved. This stage adds
authentication policies, native-provider ownership, real isolated services and
correctness coverage. It does not implement read-path optimization, pipelining
or CDC. No benchmark or profiling workload is part of this pass.

## Public connection policies

[`WithPAMDialog`](../auth_dialog.go) installs an explicit prompt callback for
MariaDB's `dialog` plugin. Callbacks receive owned prompt text, echo/last metadata
and a one-based question number. They choose the response; there is no automatic
terminal reader or password fallback. Verified TLS or a configured Unix socket
is mandatory. Prompt/response sizes, prompt count, embedded NUL and questions
after the final-question hint are checked. An empty initial plugin switch is
acknowledged without sending credentials. Completion still requires server OK.

[`WithGSSAPI`](../auth_gssapi.go) requires an exact locally configured service
principal before provider creation. Its default greeting response contains no
password proof. Password-only OK and switches to another method cannot satisfy
the policy. The plugin accepts MariaDB's old principal-only and current
principal/mechanism announcements, retaining both NUL separators. Kerberos is
supported; SPNEGO requires opt-in and a Kerberos-only credential provider. NTLM
and credential delegation are excluded. Tokens/steps are bounded, and local
mutual completion is mandatory before accepting a server OK.

The [user guide](USER_GUIDE.md#interactive-pam) covers configuration, examples,
transport, pooling and resource/error semantics. Policies are copied with the
configuration and never serialized into a DSN. Existing `RegisterAuthPlugin`
factories remain available. Optional `AuthPluginCloser` and
`AuthPluginValidator` interfaces provide exchange cleanup and local completion
validation, including failure, switch, cancellation and callback panic paths.
Built-in external plugins preserve server error code/SQLSTATE while suppressing
server prose that could echo responses or tickets.

## Optional native provider

The [`gssapi`](../gssapi/provider.go) subpackage uses the system macOS GSS
framework or Linux GSSAPI/Kerberos library. Importing only the core driver adds
no native dependency. The optional provider requires cgo; unsupported builds
return `ErrUnavailable`. Native names, credential leases, output buffers and
security contexts have one worker owner. Context cancellation and Close do not
wait for a blocked native call; the worker finishes under the OS retry policy
and releases its resources later. It never owns the SQL socket or continues
SQL authentication after cancellation. The default factory shares a 32-context
native budget, including canceled workers until their actual cleanup finishes.
Waiting for capacity honors context cancellation without starting a worker.
`gssapi.NewProvider` supplies an independent explicitly sized shared budget; SQL
pool limits alone cannot bound native work left by canceled callers.

Native source/header bytes are included in the executable fingerprint. GSSAPI
authenticates identities rather than encrypting later SQL. Configure verified
TLS for confidentiality. GSS channel bindings, Windows SSPI/NTLM and production
identity-service integration are outside this provider's current coverage.

## Isolated real authentication services

The [fixture Dockerfile](../validation/auth_fixture/Dockerfile),
[service manager](../validation/auth_fixture/serve.py) and
[PAM module](../validation/auth_fixture/pam_fixture.c) create an isolated MariaDB,
real KDC, synthetic PAM challenge policy, private service/user keys, ticket caches
and TLS CA. They do not consult or change the developer's PAM policies, accounts,
realm, credential cache or shared MariaDB global settings. Client credentials
exist only in ignored owner-only temporary directories and are removed afterward.

The Mac runner bounds the service container to two CPUs/1 GiB and publishes only
ephemeral loopback ports. Cleanup requires the exact invocation's ownership label.
Linux checks run in a disposable two-CPU/4 GiB container with a read-only source
mount and no published ports. Its local fixture mode requires
an explicit disposable-image marker. The service runtime identity, source hashes,
server version and public certificate hash are bound to the raw test-event log.
Fixture source in the actual Mac image must match this checkout; stale tags fail.
Linux retains a normal outbound route for the inherited unreachable-address
timeout assertion; module fetching is disabled with `GOPROXY=off`.
The services derive from the Haskell driver's previously tested isolated fixture,
with Go-specific realm/accounts and independent resource ownership.

Build and run on macOS, after reserving the existing SQL test schemas:

```sh
docker build -t go-mariadb-stage5-auth \
  -f validation/auth_fixture/Dockerfile validation/auth_fixture
python3 validation/auth_fixture.py -- \
  python3 validation/correctness.py \
  --output validation/results/unique-stage5-attempt --publish
python3 validation/verify.py
```

On Linux, install `libkrb5-dev` before compiling the optional package. The
additional `Dockerfile.linux` copies the Go toolchain into the service image for
isolated native-provider checks. Its `--local` fixture mode is confined to that
explicitly marked disposable image. The GitHub feature workflow provisions the
same real services; no hosted execution is claimed by local checks.

Future authorized full timing runs must also use the fixture wrapper, since
missing external-auth services are failures in validation mode. `full-v2` extends
the ordinary correctness command to include `./gssapi`; timed workload bodies
and the 262-profile catalog remain unchanged. Historical `full-v1` receipts keep
their original commands and bytes. No PAM/Kerberos timing baseline is claimed.

## Coverage and limits

Protocol tests cover dialogue kinds, empty acknowledgement, terminators, policy
and transport rejection, response/prompt limits, cancellation, callback error
identity, GSSAPI announcement/mechanism checks, credential acquisition ordering,
provider errors, incomplete mutual authentication, step/token budgets, error
redaction, switch/read failures, downgrade rejection and panic cleanup.

Provider fault tests hold native open/step work after cancellation and verify
prompt return, copied input ownership, nonblocking Close and eventual exactly-once
cleanup. Real native tests reject malformed server tokens and repeatedly release
native contexts. Live server checks cover PAM password/multiple prompts, wrong
password/second factor, protected transport, callback rejection/cancellation,
Kerberos over TCP/TLS, wrong principal/identity, missing caches and fresh contexts
across repeated connections. Existing driver coverage must remain passing.

Final full-source correctness evidence and development history are retained in
the [confirmation receipt](../validation/results/stage5-confirmation.json).
Earlier fixtures, skips and timing investigations remain explicit. Automated fixtures do not prove
compatibility with every PAM stack, enterprise realm or real application.
Readiness remains Alpha. Changes remain uncommitted; no release is authorized.

## Protocol sources

Implementation follows the checked-out MariaDB server's
`plugin/auth_pam/auth_pam_base.c`, `include/mysql/auth_dialog_client.h`,
`plugin/auth_gssapi/{client_plugin.cc,server_plugin.cc,gssapi_client.cc}` and
`sql/sql_acl.cc` authentication wrapping. Public references are MariaDB's
[connection protocol](https://mariadb.com/docs/server/reference/clientserver-protocol/1-connecting/connection),
[PAM documentation](https://mariadb.com/docs/server/reference/plugins/authentication-plugins/authentication-with-pluggable-authentication-modules-pam/authentication-plugin-pam)
and [GSSAPI documentation](https://mariadb.com/docs/server/reference/plugins/authentication-plugins/authentication-plugin-gssapi).
Source inspection and installed MariaDB runtime validation are distinct evidence.

## Initial correctness confirmation (preserved history)

Both platform runs use executable-input fingerprint
`c65a2f7f932395d71b48d621db4ea827a3f21a947600937d2bb3e6cee652171a`:
60 production/module/native files and 93 workload/CI/fixture inputs.

| Profile | PASS | FAIL | SKIP | Stage 4 passes preserved | Added passing leaves |
| --- | ---: | ---: | ---: | ---: | ---: |
| macOS arm64, Go 1.27.4, MariaDB 12.3.3 | 1,377 | 0 | 8 | 1,304 | 73 |
| Isolated Linux arm64, Go 1.27.4, MariaDB 12.3.3 | 1,380 | 0 | 5 | 1,307 | 73 |

Both pass race detection, vet and all **75 harness tests**. Native GSSAPI runs
against real Kerberos/MariaDB on both platforms; isolated Linux instrumentation
also passes. Each final receipt protects every earlier matching passing identity.
CGO-disabled provider tests pass and the Windows fallback cross-compiles; neither
establishes Windows SSPI support. Formatting, actionlint and evidence verification
pass. Restoring missing principal-pin or premature-OK validation through temporary
Go overlays makes the corresponding security regressions fail. Those controls
do not change the checked-out production bytes.

Development attempts are retained. Live PAM initially exposed its empty switch
acknowledgement. The first Linux launch put temporary files under the read-only
source mount; the next used a network profile incompatible with an inherited
timeout test and exposed an earlier cancellation-test race. The cancellation test
now waits for database/sql to observe cancellation before explicitly closing
Rows; cancellation/error and healthy subsequent connection assertions remain.
The collector's nonempty lost-pass list also exposed `int(list)` during failed
receipt finalization; it now converts a boolean and has a persistent regression.
A synthetic Python platform mock was corrected before both final confirmations.
None of these earlier failures is overwritten or presented as a passing capture.

The existing timing pointer remains byte-identical, SHA-256
`02fea12c04c5c42f1596bdb0c56ddd3839afb26a86f7e3e61f0b7e798adea7ed`.
All original/native-bulk reference checksums pass; the 62 Stage 4 profiles remain
unmeasured. No benchmark, smoke benchmark or profiling workload ran. Owned
containers and temporary ticket caches were removed. Changes remain uncommitted
on `master`; optional transaction-aware CDC is the remaining inventoried API gap.

Final CI packaging follow-up: the lint job also installs the optional GSSAPI
headers. This changes only the workflow fingerprint; every production/native/Go
workload byte matches the preceding green confirmation. Both full platform
receipts were reconfirmed after that correction, preserving the counts above.
