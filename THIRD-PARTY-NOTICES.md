# Third-Party Notices

This project links the following third-party Go packages into its distributed
binary (`optimum-gateway`, built from `./cmd`). Each is listed under its license
with a link to the license text. First-party `getoptimum` packages are omitted.

Development and build-time tooling (for example `golangci-lint`, `buf`, and
`protoc-gen-go`, invoked via the `tool` directive in `go.mod`) is **not** included
here, as it is executed as a standalone tool and is neither linked into nor
distributed with this binary.

Attribution notices required by these licenses (Apache-2.0 §4 and the upstream
`NOTICE` files it references) are reproduced in the accompanying `NOTICE` file.

Total distributed third-party packages: 142

## Summary

| License | Packages |
| --- | --- |
| MIT | 77 |
| BSD-3-Clause | 28 |
| Apache-2.0 | 25 |
| BSD-2-Clause | 5 |
| ISC | 3 |
| MPL-2.0 | 3 |
| Apache-2.0 OR MIT | 1 |

> **MPL-2.0** is a weak, file-scoped copyleft license: the copyleft reaches only
> the MPL-covered files, not this project's own source. Distributing those files,
> even unmodified, still requires preserving their notices and license and making
> their source form available to recipients.

## MIT

- [github.com/andybalholm/brotli](https://github.com/andybalholm/brotli/blob/v1.2.2/LICENSE)
- [github.com/benbjohnson/clock](https://github.com/benbjohnson/clock/blob/v1.3.5/LICENSE)
- [github.com/beorn7/perks/quantile](https://github.com/beorn7/perks/blob/v1.0.1/LICENSE)
- [github.com/cespare/xxhash/v2](https://github.com/cespare/xxhash/blob/v2.3.0/LICENSE.txt)
- [github.com/davidlazar/go-crypto/salsa20](https://github.com/davidlazar/go-crypto/blob/b73af7476f6c/LICENSE)
- [github.com/emicklei/dot](https://github.com/emicklei/dot/blob/v1.9.1/LICENSE)
- [github.com/ferranbt/fastssz](https://github.com/ferranbt/fastssz/blob/0b1f38e43198/LICENSE)
- [github.com/gofiber/fiber/v3](https://github.com/gofiber/fiber/blob/v3.4.0/LICENSE)
- [github.com/gofiber/utils/v2](https://github.com/gofiber/utils/blob/v2.1.1/LICENSE)
- [github.com/golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt/blob/v5.3.1/LICENSE)
- [github.com/ipfs/go-cid](https://github.com/ipfs/go-cid/blob/v0.5.0/LICENSE)
- [github.com/ipfs/go-datastore](https://github.com/ipfs/go-datastore/blob/v0.8.2/LICENSE)
- [github.com/ipfs/go-log](https://github.com/ipfs/go-log/blob/v1.0.5/LICENSE)
- [github.com/ipfs/go-log/tracer](https://github.com/ipfs/go-log/blob/v1.0.5/tracer/LICENSE)
- [github.com/ipfs/go-log/v2](https://github.com/ipfs/go-log/blob/v2.6.0/LICENSE)
- [github.com/ipld/go-ipld-prime](https://github.com/ipld/go-ipld-prime/blob/v0.21.0/LICENSE)
- [github.com/jbenet/go-temp-err-catcher](https://github.com/jbenet/go-temp-err-catcher/blob/v0.1.0/LICENSE)
- [github.com/klauspost/compress/zstd/internal/xxhash](https://github.com/klauspost/compress/blob/v1.19.0/zstd/internal/xxhash/LICENSE.txt)
- [github.com/klauspost/cpuid/v2](https://github.com/klauspost/cpuid/blob/v2.3.0/LICENSE)
- [github.com/koron/go-ssdp](https://github.com/koron/go-ssdp/blob/v0.0.6/LICENSE)
- [github.com/libp2p/go-buffer-pool](https://github.com/libp2p/go-buffer-pool/blob/v0.1.0/LICENSE)
- [github.com/libp2p/go-cidranger](https://github.com/libp2p/go-cidranger/blob/v1.1.0/LICENSE)
- [github.com/libp2p/go-flow-metrics](https://github.com/libp2p/go-flow-metrics/blob/v0.2.0/LICENSE)
- [github.com/libp2p/go-libp2p](https://github.com/libp2p/go-libp2p/blob/v0.48.0/LICENSE)
- [github.com/libp2p/go-libp2p-asn-util](https://github.com/libp2p/go-libp2p-asn-util/blob/v0.4.1/LICENSE)
- [github.com/libp2p/go-libp2p-kad-dht](https://github.com/libp2p/go-libp2p-kad-dht/blob/v0.33.1/LICENSE)
- [github.com/libp2p/go-libp2p-kbucket](https://github.com/libp2p/go-libp2p-kbucket/blob/v0.7.0/LICENSE)
- [github.com/libp2p/go-libp2p-mplex](https://github.com/libp2p/go-libp2p-mplex/blob/v0.11.0/LICENSE)
- [github.com/libp2p/go-libp2p-record](https://github.com/libp2p/go-libp2p-record/blob/v0.3.1/LICENSE)
- [github.com/libp2p/go-libp2p-routing-helpers/tracing](https://github.com/libp2p/go-libp2p-routing-helpers/blob/v0.7.5/LICENSE)
- [github.com/libp2p/go-mplex](https://github.com/libp2p/go-mplex/blob/v0.7.0/LICENSE)
- [github.com/libp2p/go-msgio](https://github.com/libp2p/go-msgio/blob/v0.3.0/LICENSE)
- [github.com/mattn/go-colorable](https://github.com/mattn/go-colorable/blob/v0.1.15/LICENSE)
- [github.com/mattn/go-isatty](https://github.com/mattn/go-isatty/blob/v0.0.22/LICENSE)
- [github.com/mitchellh/mapstructure](https://github.com/mitchellh/mapstructure/blob/v1.5.0/LICENSE)
- [github.com/montanaflynn/stats](https://github.com/montanaflynn/stats/blob/v0.7.1/LICENSE)
- [github.com/mr-tron/base58/base58](https://github.com/mr-tron/base58/blob/v1.3.0/LICENSE)
- [github.com/multiformats/go-multiaddr](https://github.com/multiformats/go-multiaddr/blob/v0.16.1/LICENSE)
- [github.com/multiformats/go-multiaddr-dns](https://github.com/multiformats/go-multiaddr-dns/blob/v0.4.1/LICENSE)
- [github.com/multiformats/go-multiaddr-fmt](https://github.com/multiformats/go-multiaddr-fmt/blob/v0.1.0/LICENSE)
- [github.com/multiformats/go-multibase](https://github.com/multiformats/go-multibase/blob/v0.2.0/LICENSE)
- [github.com/multiformats/go-multihash](https://github.com/multiformats/go-multihash/blob/v0.2.3/LICENSE)
- [github.com/multiformats/go-multistream](https://github.com/multiformats/go-multistream/blob/v0.6.1/LICENSE)
- [github.com/multiformats/go-varint](https://github.com/multiformats/go-varint/blob/v0.1.0/LICENSE)
- [github.com/philhofer/fwd](https://github.com/philhofer/fwd/blob/v1.2.0/LICENSE.md)
- [github.com/pion/datachannel](https://github.com/pion/datachannel/blob/v1.5.10/LICENSE)
- [github.com/pion/dtls/v3](https://github.com/pion/dtls/blob/v3.1.2/LICENSE)
- [github.com/pion/ice/v4](https://github.com/pion/ice/blob/v4.0.10/LICENSE)
- [github.com/pion/interceptor](https://github.com/pion/interceptor/blob/v0.1.40/LICENSE)
- [github.com/pion/logging](https://github.com/pion/logging/blob/v0.2.4/LICENSE)
- [github.com/pion/mdns/v2](https://github.com/pion/mdns/blob/v2.0.7/LICENSE)
- [github.com/pion/randutil](https://github.com/pion/randutil/blob/v0.1.0/LICENSE)
- [github.com/pion/rtcp](https://github.com/pion/rtcp/blob/v1.2.16/LICENSE)
- [github.com/pion/rtp](https://github.com/pion/rtp/blob/v1.8.19/LICENSE)
- [github.com/pion/sctp](https://github.com/pion/sctp/blob/v1.8.39/LICENSE)
- [github.com/pion/sdp/v3](https://github.com/pion/sdp/blob/v3.0.18/LICENSE)
- [github.com/pion/srtp/v3](https://github.com/pion/srtp/blob/v3.0.6/LICENSE)
- [github.com/pion/stun/v3](https://github.com/pion/stun/blob/v3.1.1/LICENSE)
- [github.com/pion/transport/v3](https://github.com/pion/transport/blob/v3.0.7/LICENSE)
- [github.com/pion/transport/v4](https://github.com/pion/transport/blob/v4.0.1/LICENSE)
- [github.com/pion/turn/v4](https://github.com/pion/turn/blob/v4.0.2/LICENSE)
- [github.com/pion/webrtc/v4](https://github.com/pion/webrtc/blob/v4.1.2/LICENSE)
- [github.com/polydawn/refmt](https://github.com/polydawn/refmt/blob/40501e09de1f/LICENSE)
- [github.com/quic-go/qpack](https://github.com/quic-go/qpack/blob/v0.6.0/LICENSE.md)
- [github.com/quic-go/quic-go](https://github.com/quic-go/quic-go/blob/v0.59.1/LICENSE)
- [github.com/quic-go/webtransport-go](https://github.com/quic-go/webtransport-go/blob/v0.10.0/LICENSE)
- [github.com/tinylib/msgp/msgp](https://github.com/tinylib/msgp/blob/v1.6.4/LICENSE)
- [github.com/valyala/bytebufferpool](https://github.com/valyala/bytebufferpool/blob/v1.0.0/LICENSE)
- [github.com/valyala/fasthttp](https://github.com/valyala/fasthttp/blob/v1.72.0/LICENSE)
- [github.com/valyala/fasthttp/reuseport](https://github.com/valyala/fasthttp/blob/v1.72.0/reuseport/LICENSE)
- [github.com/whyrusleeping/go-keyspace](https://github.com/whyrusleeping/go-keyspace/blob/5b898ac5add1/LICENSE)
- [go.uber.org/dig](https://github.com/uber-go/dig/blob/v1.19.0/LICENSE)
- [go.uber.org/fx](https://github.com/uber-go/fx/blob/v1.24.0/LICENSE)
- [go.uber.org/multierr](https://github.com/uber-go/multierr/blob/v1.11.0/LICENSE.txt)
- [go.uber.org/zap](https://github.com/uber-go/zap/blob/v1.28.0/LICENSE)
- [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml/blob/v3.0.1/LICENSE)
- [lukechampine.com/blake3](https://github.com/lukechampine/blake3/blob/v1.4.1/LICENSE)

## BSD-3-Clause

- [filippo.io/bigmod](https://github.com/FiloSottile/bigmod/blob/f8a47775ebe5/LICENSE)
- [github.com/andybalholm/brotli/flate](https://github.com/andybalholm/brotli/blob/v1.2.2/flate/LICENSE)
- [github.com/dunglas/httpsfv](https://github.com/dunglas/httpsfv/blob/v1.1.0/LICENSE)
- [github.com/flynn/noise](https://github.com/flynn/noise/blob/v1.1.0/LICENSE)
- [github.com/gofiber/schema](https://github.com/gofiber/schema/blob/v1.8.0/LICENSE)
- [github.com/gogo/protobuf/proto](https://github.com/gogo/protobuf/blob/v1.3.2/LICENSE)
- [github.com/golang/snappy](https://github.com/golang/snappy/blob/v1.0.0/LICENSE)
- [github.com/google/gopacket/routing](https://github.com/google/gopacket/blob/v1.1.19/LICENSE)
- [github.com/google/uuid](https://github.com/google/uuid/blob/v1.6.0/LICENSE)
- [github.com/hashicorp/golang-lru/v2/simplelru](https://github.com/hashicorp/golang-lru/blob/v2.0.7/simplelru/LICENSE_list)
- [github.com/klauspost/compress/internal/snapref](https://github.com/klauspost/compress/blob/v1.19.0/internal/snapref/LICENSE)
- [github.com/libp2p/go-netroute](https://github.com/libp2p/go-netroute/blob/v0.4.0/LICENSE)
- [github.com/miekg/dns](https://github.com/miekg/dns/blob/v1.1.66/LICENSE)
- [github.com/multiformats/go-base32](https://github.com/multiformats/go-base32/blob/v0.1.0/LICENSE)
- [github.com/munnerz/goautoneg](https://github.com/munnerz/goautoneg/blob/a7dc8b61c822/LICENSE)
- [github.com/pbnjay/memory](https://github.com/pbnjay/memory/blob/7b4eea64cf58/LICENSE)
- [github.com/prometheus/client_golang/internal/github.com/golang/gddo/httputil](https://github.com/prometheus/client_golang/blob/v1.23.2/internal/github.com/golang/gddo/LICENSE)
- [github.com/spaolacci/murmur3](https://github.com/spaolacci/murmur3/blob/v1.1.0/LICENSE)
- [github.com/wlynxg/anet](https://github.com/wlynxg/anet/blob/v0.0.5/LICENSE)
- [golang.org/x/crypto](https://cs.opensource.google/go/x/crypto/+/v0.53.0:LICENSE)
- [golang.org/x/exp/slices](https://cs.opensource.google/go/x/exp/+/74f9aab9:LICENSE)
- [golang.org/x/net](https://cs.opensource.google/go/x/net/+/v0.56.0:LICENSE)
- [golang.org/x/sync/errgroup](https://cs.opensource.google/go/x/sync/+/v0.21.0:LICENSE)
- [golang.org/x/sys](https://cs.opensource.google/go/x/sys/+/v0.46.0:LICENSE)
- [golang.org/x/text](https://cs.opensource.google/go/x/text/+/v0.38.0:LICENSE)
- [golang.org/x/time/rate](https://cs.opensource.google/go/x/time/+/v0.14.0:LICENSE)
- [gonum.org/v1/gonum/mathext](https://github.com/gonum/gonum/blob/v0.17.0/LICENSE)
- [google.golang.org/protobuf](https://github.com/protocolbuffers/protobuf-go/blob/v1.36.11/LICENSE)

## Apache-2.0

- [github.com/MicahParks/jwkset](https://github.com/MicahParks/jwkset/blob/v0.11.0/LICENSE)
- [github.com/MicahParks/keyfunc/v3](https://github.com/MicahParks/keyfunc/blob/v3.8.0/LICENSE)
- [github.com/go-logr/logr](https://github.com/go-logr/logr/blob/v1.4.3/LICENSE)
- [github.com/go-logr/stdr](https://github.com/go-logr/stdr/blob/v1.2.2/LICENSE)
- [github.com/ipfs/boxo](https://github.com/ipfs/boxo/blob/v0.30.0/LICENSE.md)
- [github.com/jackpal/go-nat-pmp](https://github.com/jackpal/go-nat-pmp/blob/v1.0.2/LICENSE)
- [github.com/klauspost/compress](https://github.com/klauspost/compress/blob/v1.19.0/LICENSE)
- [github.com/libp2p/go-libp2p-pubsub](https://github.com/libp2p/go-libp2p-pubsub/blob/41b11d5cb1a7/LICENSE-APACHE)
- [github.com/libp2p/go-libp2p/p2p/net/nat/internal/nat](https://github.com/libp2p/go-libp2p/blob/v0.48.0/p2p/net/nat/internal/nat/LICENSE)
- [github.com/libp2p/go-libp2p/p2p/transport/websocket](https://github.com/libp2p/go-libp2p/blob/v0.48.0/p2p/transport/websocket/LICENSE-APACHE)
- [github.com/minio/sha256-simd](https://github.com/minio/sha256-simd/blob/v1.0.1/LICENSE)
- [github.com/multiformats/go-multicodec](https://github.com/multiformats/go-multicodec/blob/v0.9.2/LICENSE-APACHE)
- [github.com/opentracing/opentracing-go](https://github.com/opentracing/opentracing-go/blob/v1.2.0/LICENSE)
- [github.com/prometheus/client_golang/prometheus](https://github.com/prometheus/client_golang/blob/v1.23.2/LICENSE)
- [github.com/prometheus/client_model/go](https://github.com/prometheus/client_model/blob/v0.6.2/LICENSE)
- [github.com/prometheus/common](https://github.com/prometheus/common/blob/v0.67.4/LICENSE)
- [github.com/prometheus/procfs](https://github.com/prometheus/procfs/blob/v0.19.2/LICENSE)
- [go.opentelemetry.io/auto/sdk](https://github.com/open-telemetry/opentelemetry-go-instrumentation/blob/sdk/v1.2.1/sdk/LICENSE)
- [go.opentelemetry.io/otel](https://github.com/open-telemetry/opentelemetry-go/blob/v1.43.0/LICENSE)
- [go.opentelemetry.io/otel/metric](https://github.com/open-telemetry/opentelemetry-go/blob/metric/v1.43.0/metric/LICENSE)
- [go.opentelemetry.io/otel/trace](https://github.com/open-telemetry/opentelemetry-go/blob/trace/v1.43.0/trace/LICENSE)
- [go.yaml.in/yaml/v2](https://github.com/yaml/go-yaml/blob/v2.4.3/LICENSE)
- [google.golang.org/genproto/googleapis/rpc/status](https://github.com/googleapis/go-genproto/blob/0a33c5d7ca68/googleapis/rpc/LICENSE)
- [google.golang.org/grpc](https://github.com/grpc/grpc-go/blob/v1.81.1/LICENSE)
- [gopkg.in/yaml.v2](https://github.com/go-yaml/yaml/blob/v2.4.0/LICENSE)

## BSD-2-Clause

- [github.com/gorilla/websocket](https://github.com/gorilla/websocket/blob/v1.5.3/LICENSE)
- [github.com/huin/goupnp](https://github.com/huin/goupnp/blob/v1.3.0/LICENSE)
- [github.com/marten-seemann/tcp](https://github.com/marten-seemann/tcp/blob/dfbc87cc63fd/LICENSE)
- [github.com/mikioh/tcpinfo](https://github.com/mikioh/tcpinfo/blob/30a79bb1804b/LICENSE)
- [github.com/mikioh/tcpopt](https://github.com/mikioh/tcpopt/blob/172688c1accc/LICENSE)

## ISC

- [filippo.io/keygen](https://github.com/FiloSottile/keygen/blob/8e2790ea4c5b/LICENSE)
- [github.com/decred/dcrd/dcrec/secp256k1/v4](https://github.com/decred/dcrd/blob/dcrec/secp256k1/v4.4.0/dcrec/secp256k1/LICENSE)
- [github.com/libp2p/go-reuseport](https://github.com/libp2p/go-reuseport/blob/v0.4.0/LICENSE)

## MPL-2.0

- [github.com/hashicorp/golang-lru/simplelru](https://github.com/hashicorp/golang-lru/blob/v1.0.2/LICENSE)
- [github.com/hashicorp/golang-lru/v2](https://github.com/hashicorp/golang-lru/blob/v2.0.7/LICENSE)
- [github.com/libp2p/go-yamux/v5](https://github.com/libp2p/go-yamux/blob/v5.0.1/LICENSE)

## Apache-2.0 OR MIT

- [github.com/multiformats/go-base36](https://pkg.go.dev/github.com/multiformats/go-base36?tab=licenses)
