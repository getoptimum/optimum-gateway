# Test identity keys

Copies of the two libp2p identities in `../../datagram-demo/keys/`, duplicated so
this tier builds its own image from its own context and does not depend on the
five-gateway stack having been built first. See that directory's README for the
on-disk format.

| File | Used by | Peer ID |
|---|---|---|
| `gateway1-libp2p.p2p.key` | gateway1's consensus-layer libp2p host | `12D3KooWEDTWcWiackMyvjvAUut5kk9acyZUnNEuoALFrs7fWH41` |
| `publisher-cl.p2p.key` | the publisher | `12D3KooWPDjNxSEBqze1kBvHjwoqFFvmiG7VCq2VVC3X5rgDJRPr` |

They are fixed because the publisher dials gateway1 by peer ID without discovery
and gateway1's `OPT_DIRECT_CL_PEERS` allowlist pins the publisher's. Both peer IDs
are in `gen-stack.py`, so regenerating a key means updating it there too.

These are **committed private keys** with no access to any network, service or
account. Do not reuse them anywhere that matters.

The mump2p mesh identities are not pre-generated: each gateway creates its own on
first start, which is also what makes its JWT subject, and therefore its
`gateway_id`, unique. That identity is the `mump2p.node_id` the spans are tagged
with, and `run.sh` reads it back out of each gateway's startup log.
