/* Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0 */
#ifndef MARIADB_GO_GSS_NATIVE_H
#define MARIADB_GO_GSS_NATIVE_H
#include <stdint.h>
#include <stddef.h>
struct mariadb_gss_context;
int mariadb_gss_open(const void *, size_t, int, struct mariadb_gss_context **, uint32_t *, uint32_t *);
int mariadb_gss_step(struct mariadb_gss_context *, const void *, size_t, void **, size_t *, int *, uint32_t *, uint32_t *);
void mariadb_gss_close(struct mariadb_gss_context *);
uint32_t mariadb_gss_live_contexts(void);
#endif
