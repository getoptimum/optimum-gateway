# Demo identity keys

Two libp2p Ed25519 private keys in the on-disk JSON format
`optimum-common/pkg/identity` expects:

```json
{ "Key": "<protobuf-marshaled libp2p PrivKey>", "ID": "<peer ID>" }
```

wrapped by an 8-byte CRC64-ISO prefix, which is what `identity.LoadFromFile`
checks.

| File | Used by | Peer ID |
|---|---|---|
| `gateway1-libp2p.p2p.key` | gateway1's consensus-layer libp2p host | `12D3KooWEDTWcWiackMyvjvAUut5kk9acyZUnNEuoALFrs7fWH41` |
| `publisher-cl.p2p.key` | the demo publisher | `12D3KooWPDjNxSEBqze1kBvHjwoqFFvmiG7VCq2VVC3X5rgDJRPr` |

They are fixed because the publisher dials gateway1 by peer ID without
discovery, and gateway1's `OPT_DIRECT_CL_PEERS` allowlist pins the publisher's.
Both peer IDs are hardcoded in `../docker-compose.yml`, so regenerating a key
means updating it there too.

These are **committed private keys**. Anyone with this repo can impersonate these
two peer IDs. That is acceptable only because they exist solely for this local
demo and grant no access to any network, service or account. Do not reuse them
anywhere that matters.

The mump2p mesh identities are not pre-generated: each gateway creates its own on
first start, which is also what makes its JWT subject, and therefore its
`gateway_id`, unique.
