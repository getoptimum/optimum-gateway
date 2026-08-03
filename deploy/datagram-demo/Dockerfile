# One image, three entrypoints: the gateway, the mock control plane and the CL
# publisher. Compose picks between them per service.
#
# The binaries are built on the host by run.sh rather than in a builder stage
# here: the repo go.mod carries a filesystem `replace` for mump2p-protocol that
# points outside any plausible build context, so an in-image `go build` cannot
# resolve it. Building outside and copying in keeps the demo runnable from a
# working tree that has that replace in place, which is the whole point of it.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY bin/optimum-gateway bin/mock-bootstrap bin/bench-traffic /usr/local/bin/
COPY keys/ /keys/
COPY gateway-entrypoint.sh /usr/local/bin/gateway-entrypoint.sh

# /gateway/logs is the gateway's rotating debug-log directory. It falls back to
# stdout-only when the directory is not writable, so this is about keeping the
# run quiet rather than about correctness.
RUN adduser -D -u 10001 demo \
 && mkdir -p /gateway/logs \
 && chown -R 10001:10001 /gateway \
 && chmod 0755 /usr/local/bin/optimum-gateway /usr/local/bin/mock-bootstrap \
               /usr/local/bin/bench-traffic /usr/local/bin/gateway-entrypoint.sh

# Unprivileged: identity dirs and the JWKS cache live under /tmp, /keys is
# read-only, and the shared-memory lanes are owned by the same uid because the
# rlnc-server sidecar runs as it too (see docker-compose.yml).
USER 10001
