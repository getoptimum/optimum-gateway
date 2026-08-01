FROM --platform=$BUILDPLATFORM tonistiigi/xx:latest AS xx

FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM

RUN apk add --no-cache build-base git clang lld openssh-client

COPY --from=xx / /

RUN xx-go --wrap

ENV CGO_ENABLED=1
ENV CC=xx-clang CXX=xx-clang++

RUN xx-apk --no-cache add musl-dev g++

WORKDIR /optimum-gateway

RUN --mount=type=ssh,required=false \
    --mount=type=secret,id=github_token,required=false \
    if [ -f /run/secrets/github_token ]; then \
        git config --global url."https://x-access-token:$(tr -d '[:space:]' < /run/secrets/github_token)@github.com/".insteadOf "https://github.com/"; \
    elif [ -n "$SSH_AUTH_SOCK" ] || [ -f /run/buildkit/ssh_agent.0 ]; then \
        git config --global url."git@github.com:".insteadOf "https://github.com/" && \
        mkdir -p -m 0700 ~/.ssh && \
        ssh-keyscan github.com >> ~/.ssh/known_hosts 2>/dev/null || true; \
    else \
        echo "ERROR: Neither github_token nor SSH authentication available"; \
        exit 1; \
    fi

ENV GOPRIVATE=github.com/getoptimum
ENV GONOSUMDB=github.com/getoptimum

COPY go.mod go.sum ./
RUN --mount=type=ssh,required=false \
    --mount=type=secret,id=github_token,required=false \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o /gateway/optimum-gateway ./cmd

FROM alpine:3.22

# The RLNC coder runs out of process, so this image is one half of a pair: it will
# not start unless a getoptimum/rlnc-server container shares its /dev/shm. The
# coder is deliberately not bundled here, so that the gateway stays a single
# process under an init-friendly entrypoint and the two can be restarted and
# rolled independently. Kept in step with RLNC_IMAGE_VERSION in the Makefile.
ARG RLNC_IMAGE_VERSION=v0.10.0
LABEL xyz.getoptimum.rlnc-coder.image="getoptimum/rlnc-server:${RLNC_IMAGE_VERSION}" \
      xyz.getoptimum.rlnc-coder.shm-lanes="20" \
      xyz.getoptimum.rlnc-coder.shm-bytes-min="335544800"

RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates

# TODO: Enable custom user for improved security once migration path is documented
# Partners would need to: chown -R 1000:1000 /path/to/p2p/keys /path/to/logs
# RUN addgroup -g 1000 gateway && \
#     adduser -D -u 1000 -G gateway gateway

WORKDIR /gateway

RUN mkdir -p /gateway/logs

COPY --from=builder /gateway/optimum-gateway /optimum-gateway

# License and third-party attribution shipped alongside the binary.
COPY --from=builder /optimum-gateway/LICENSE /optimum-gateway/NOTICE /optimum-gateway/THIRD-PARTY-NOTICES.md /usr/share/doc/optimum-gateway/

# USER gateway

EXPOSE 33212 33213 48123

ENTRYPOINT ["/optimum-gateway"]
