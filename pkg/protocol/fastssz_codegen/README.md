FastSSZ spectests types for Ethereum consensus

This package vendors the fastssz `spectests` package: Go types with
pre-generated SSZ encoders/decoders for Ethereum consensus structures.
It is used in production by `pkg/protocol/consensus` to decode beacon
block headers from gossip payloads, not only in tests.

The package name stays `spectests` (not matching the directory) on
purpose: files are copied verbatim from upstream so that refreshes stay
a plain copy and diffs against upstream stay empty. For the same reason
some upstream helpers (e.g. `setMinimalSpec`) are unused here.

Regenerating
- From the repo root: `make fastssz-generate`
- This copies the package from the go.mod-pinned `github.com/ferranbt/fastssz`
  module and drops test files plus unused test helpers. To upgrade, bump the
  dependency in `go.mod` and re-run the target.
- CI verifies the vendored files match the pinned module and refreshes them
  on drift.

Do not edit these files by hand; re-run the target instead.
