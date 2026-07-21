#!/usr/bin/env python3
"""
normalize-sbom.py: strip per-build noise from a CycloneDX SBOM so the committed
copy is stable across commits and machines.

cyclonedx-gomod stamps three things into the SBOM that change without any
dependency change, which would otherwise make the CI sbom-refresh job commit on
every push:

  1. the self module's VCS pseudo-version (new git commit -> new version),
  2. the generator tool's binary hashes (differ per build host),
  3. the GOVERSION build-env property (changes on a Go toolchain bump).

None of these describe a third-party dependency. This script pins them so drift
only appears when the actual dependency set changes. It is idempotent and must
be run by `make sbom` on both developer machines and CI so both sides produce
identical output.

Usage: normalize-sbom.py <sbom.json> [<sbom.json> ...]
"""

import json
import sys

SELF_VERSION = "0.0.0-dev"
GOVERSION_PROP = "cdx:gomod:build:env:GOVERSION"


def _strip_goversion(component):
    props = component.get("properties")
    if props:
        component["properties"] = [p for p in props if p.get("name") != GOVERSION_PROP]


def normalize(path):
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)

    comp = doc.get("metadata", {}).get("component", {})
    real_version = comp.get("version")
    old_purl = comp.get("purl")
    old_ref = comp.get("bom-ref")

    # Drop the generator tool's binary hashes (list or object schema).
    tools = doc.get("metadata", {}).get("tools")
    if isinstance(tools, list):
        for tool in tools:
            tool.pop("hashes", None)
    elif isinstance(tools, dict):
        for tool in tools.get("components", []):
            tool.pop("hashes", None)

    # Drop the toolchain version property from the self component and all deps.
    _strip_goversion(comp)
    for component in doc.get("components", []):
        _strip_goversion(component)

    # Pin the self module's VCS pseudo-version. Set the version field
    # structurally, then replace only the full self purl/bom-ref strings, which
    # embed the module path and are unique to the self component. A bare global
    # replace of the version could corrupt a dependency sharing that version on
    # a tagged build.
    pinning = bool(real_version) and real_version != SELF_VERSION
    if pinning:
        comp["version"] = SELF_VERSION
        for component in doc.get("components", []):
            if component.get("bom-ref") == old_ref:
                component["version"] = SELF_VERSION

    out = json.dumps(doc, indent=2, ensure_ascii=False)

    if pinning:
        if old_ref:
            out = out.replace(old_ref, old_ref.replace("@" + real_version, "@" + SELF_VERSION))
        if old_purl:
            out = out.replace(old_purl, old_purl.replace("@" + real_version, "@" + SELF_VERSION))

    with open(path, "w", encoding="utf-8") as fh:
        fh.write(out)
        fh.write("\n")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: normalize-sbom.py <sbom.json> [...]", file=sys.stderr)
        sys.exit(2)
    for arg in sys.argv[1:]:
        normalize(arg)
