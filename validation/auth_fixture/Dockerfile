# Authentication services only; the client under test runs on the host.
FROM mariadb:12.3@sha256:d46e102fa8b9b72bfcdeb56f9dd538742092119dc8b11f31fe662605cd234cd9
USER root
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    krb5-kdc krb5-admin-server krb5-user libpam0g-dev gcc libc6-dev python3 openssl \
    mariadb-plugin-gssapi-server \
    && rm -rf /var/lib/apt/lists/*
COPY pam_fixture.c /opt/driver-auth/pam_fixture.c
RUN gcc -Wall -Wextra -Werror -fPIC -shared /opt/driver-auth/pam_fixture.c \
    -lpam -o /opt/driver-auth/pam_fixture.so
COPY serve.py /opt/driver-auth/serve.py
ENTRYPOINT ["python3", "/opt/driver-auth/serve.py"]
