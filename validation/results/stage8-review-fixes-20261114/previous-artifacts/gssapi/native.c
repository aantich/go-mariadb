//go:build cgo && (darwin || linux)

/* Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0
 * One Go worker exclusively owns this state. No native function reads or writes
 * the SQL socket. Go copies output before the next call releases it. */
#include "native.h"
#ifdef __APPLE__
#include <GSS/GSS.h>
#else
#include <gssapi/gssapi.h>
#endif
#include <stdlib.h>
#include <stdatomic.h>

struct mariadb_gss_context {
    gss_name_t service;
    gss_ctx_id_t context;
    gss_cred_id_t credentials;
    gss_buffer_desc output;
    gss_OID_desc mechanism;
    int counted;
};
static _Atomic uint32_t live_contexts;
uint32_t mariadb_gss_live_contexts(void) { return atomic_load(&live_contexts); }

void mariadb_gss_close(struct mariadb_gss_context *state)
{
    OM_uint32 minor;
    if (!state) return;
    if (state->output.value) gss_release_buffer(&minor, &state->output);
    if (state->context != GSS_C_NO_CONTEXT)
        gss_delete_sec_context(&minor, &state->context, GSS_C_NO_BUFFER);
    if (state->credentials != GSS_C_NO_CREDENTIAL)
        gss_release_cred(&minor, &state->credentials);
    if (state->service != GSS_C_NO_NAME) gss_release_name(&minor, &state->service);
    if (state->counted) atomic_fetch_sub(&live_contexts, 1);
    free(state);
}

int mariadb_gss_open(const void *principal, size_t length, int negotiate,
                    struct mariadb_gss_context **result, uint32_t *major, uint32_t *minor)
{
    static unsigned char kerberos[] = {0x2a,0x86,0x48,0x86,0xf7,0x12,0x01,0x02,0x02};
    static unsigned char spnego[] = {0x2b,0x06,0x01,0x05,0x05,0x02};
    gss_OID_desc kerberos_oid = {sizeof(kerberos), kerberos};
    gss_OID_set_desc allowed = {1, &kerberos_oid};
    gss_buffer_desc name = {length, (void *)principal};
    struct mariadb_gss_context *state = calloc(1, sizeof(*state));
    *result = NULL;
    *major = *minor = 0;
    if (!state) return 1;
    state->service = GSS_C_NO_NAME;
    state->context = GSS_C_NO_CONTEXT;
    state->credentials = GSS_C_NO_CREDENTIAL;
    state->mechanism.length = negotiate ? sizeof(spnego) : sizeof(kerberos);
    state->mechanism.elements = negotiate ? spnego : kerberos;
    *major = gss_import_name(minor, &name, GSS_C_NT_USER_NAME, &state->service);
    if (GSS_ERROR(*major)) { mariadb_gss_close(state); return 2; }
    /* Restrict acquired credentials to Kerberos even if SPNEGO is explicitly
     * enabled. Never try a cached NTLM identity or delegate the client's TGT. */
    *major = gss_acquire_cred(minor, GSS_C_NO_NAME, 0, &allowed, GSS_C_INITIATE,
                            &state->credentials, NULL, NULL);
    if (GSS_ERROR(*major)) { mariadb_gss_close(state); return 2; }
    state->counted = 1;
    atomic_fetch_add(&live_contexts, 1);
    *result = state;
    return 0;
}

int mariadb_gss_step(struct mariadb_gss_context *state, const void *input, size_t length,
                    void **output, size_t *output_length, int *complete,
                    uint32_t *major, uint32_t *minor)
{
    OM_uint32 flags = 0, ignored;
    gss_buffer_desc token = {length, (void *)input};
    if (state->output.value) gss_release_buffer(&ignored, &state->output);
    state->output.value = NULL;
    state->output.length = 0;
    *output = NULL;
    *output_length = 0;
    *complete = 0;
    *major = gss_init_sec_context(minor, state->credentials, &state->context,
        state->service, &state->mechanism, GSS_C_MUTUAL_FLAG, 0,
        GSS_C_NO_CHANNEL_BINDINGS, length ? &token : GSS_C_NO_BUFFER,
        NULL, &state->output, &flags, NULL);
    if (GSS_ERROR(*major)) return 2;
    if (state->output.length > 1048576) return 3;
    *complete = (*major & GSS_S_CONTINUE_NEEDED) == 0;
    if (*complete && !(flags & GSS_C_MUTUAL_FLAG)) return 4;
    *output = state->output.value;
    *output_length = state->output.length;
    return 0;
}
