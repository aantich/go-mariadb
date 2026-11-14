/* A deterministic PAM module for a disposable test server. These are synthetic
 * fixture credentials; no host OS account or password database is consulted. */
#include <security/pam_modules.h>
#include <security/pam_appl.h>
#include <stdlib.h>
#include <string.h>

static int question(pam_handle_t *pamh, int style, const char *prompt,
                    const char *expected)
{
    const struct pam_conv *conversation;
    struct pam_message message = {style, prompt};
    const struct pam_message *messages[] = {&message};
    struct pam_response *responses = NULL;
    int status = pam_get_item(pamh, PAM_CONV, (const void **)&conversation);
    if (status != PAM_SUCCESS) return status;
    status = conversation->conv(1, messages, &responses, conversation->appdata_ptr);
    if (status == PAM_SUCCESS) {
        status = responses && responses[0].resp &&
            strcmp(responses[0].resp, expected) == 0 ? PAM_SUCCESS : PAM_AUTH_ERR;
    }
    if (responses) {
        free(responses[0].resp);
        free(responses);
    }
    return status;
}

PAM_EXTERN int pam_sm_authenticate(pam_handle_t *pamh, int flags,
                                  int argc, const char **argv)
{
    (void)flags;
    int status = question(pamh, PAM_PROMPT_ECHO_OFF, "Fixture password:",
                          "stage7-test-password");
    if (status == PAM_SUCCESS && argc > 0 && strcmp(argv[0], "two-factor") == 0)
        status = question(pamh, PAM_PROMPT_ECHO_ON, "Fixture code:", "246810");
    return status;
}

PAM_EXTERN int pam_sm_setcred(pam_handle_t *pamh, int flags,
                             int argc, const char **argv)
{
    (void)pamh; (void)flags; (void)argc; (void)argv;
    return PAM_SUCCESS;
}
