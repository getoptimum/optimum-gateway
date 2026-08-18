FROM --platform=$BUILDPLATFORM tonistiigi/xx:latest AS xx

FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM

RUN apk add --no-cache build-base git clang lld openssh-client bash

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

RUN make RLNC_SERVER_OUTPUT=/gateway/bin/rlnc-server build-rlnc-server

FROM alpine:3.22

RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates

# TODO: Enable custom user for improved security once migration path is documented
# Partners would need to: chown -R 1000:1000 /path/to/p2p/keys /path/to/logs
# RUN addgroup -g 1000 gateway && \
#     adduser -D -u 1000 -G gateway gateway

WORKDIR /gateway

RUN mkdir -p /gateway/logs

COPY --from=builder /gateway/optimum-gateway /optimum-gateway
COPY --from=builder /gateway/bin/rlnc-server /rlnc-server

# License, patent marking and third-party attribution shipped with the binary.
COPY --from=builder /optimum-gateway/LICENSE /optimum-gateway/NOTICE /optimum-gateway/PATENTS /optimum-gateway/THIRD-PARTY-NOTICES.md /usr/share/doc/optimum-gateway/

# USER gateway
RUN /rlnc-server --lanes 20 --name mump2p-protocol &

EXPOSE 33212 33213 48123

ENTRYPOINT ["/optimum-gateway"]
