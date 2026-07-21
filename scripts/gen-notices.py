#!/usr/bin/env python3
"""
gen-notices.py: build THIRD-PARTY-NOTICES.md from go-licenses CSV output.

Usage:
    go-licenses csv ./cmd | python3 scripts/gen-notices.py --config licenses.yaml \
        --output THIRD-PARTY-NOTICES.md

Lists every third-party package linked into the shipped binary (./cmd), grouped
by license with a link to the upstream license text. First-party getoptimum
packages are omitted (proprietary, not third-party license risk). Packages that
go-licenses reports as "Unknown" are resolved via the `exceptions` block in
licenses.yaml, the same source the license-check gate uses.
"""

import argparse
import csv
import importlib.util
import os
import sys

FIRST_PARTY_PREFIX = "github.com/getoptimum/"

# go-licenses cannot resolve the license URL for some vanity import paths: it
# returns "Unknown" in CI while resolving it locally, which makes the notices
# file non-deterministic. Pin the canonical license-text URL so both agree.
# Update the version when the dependency is bumped.
URL_OVERRIDES = {
    "gonum.org/v1/gonum/mathext": "https://github.com/gonum/gonum/blob/v0.17.0/LICENSE",
}


def load_exceptions(config_path):
    """Reuse check-licenses.py's stdlib YAML parser so both read one source."""
    here = os.path.dirname(os.path.abspath(__file__))
    spec = importlib.util.spec_from_file_location(
        "check_licenses", os.path.join(here, "check-licenses.py")
    )
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    with open(config_path) as f:
        data = mod._parse_yaml(f.read())
    out = {}
    for e in data.get("exceptions", []) or []:
        if isinstance(e, dict) and e.get("package"):
            out[e["package"]] = e.get("license", "Unknown")
    return out


def pkg_go_dev_url(pkg):
    return f"https://pkg.go.dev/{pkg}?tab=licenses"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--config", default="licenses.yaml")
    ap.add_argument("--output", default="-")
    ap.add_argument("csv_file", nargs="?", help="CSV file (default: stdin)")
    args = ap.parse_args()

    exceptions = load_exceptions(args.config)
    src = open(args.csv_file) if args.csv_file else sys.stdin

    groups = {}  # license -> {pkg: url}
    for row in csv.reader(src):
        if len(row) < 3:
            continue
        pkg, url, lic = row[0].strip(), row[1].strip(), row[2].strip()
        if pkg.startswith(FIRST_PARTY_PREFIX):
            continue
        if lic == "Unknown":
            mapped = exceptions.get(pkg)
            if not mapped:
                print(f"error: unresolved Unknown license: {pkg}", file=sys.stderr)
                return 1
            lic = mapped
            if url == "Unknown":
                url = pkg_go_dev_url(pkg)
        url = URL_OVERRIDES.get(pkg, url)
        groups.setdefault(lic, {})[pkg] = url

    total = sum(len(v) for v in groups.values())
    # Deterministic order: by package count desc, then license name.
    ordered = sorted(groups.items(), key=lambda kv: (-len(kv[1]), kv[0]))

    out = open(args.output, "w") if args.output != "-" else sys.stdout
    w = out.write
    w("# Third-Party Notices\n\n")
    w("This project links the following third-party Go packages into its distributed\n")
    w("binary (`optimum-gateway`, built from `./cmd`). Each is listed under its license\n")
    w("with a link to the license text. First-party `getoptimum` packages are omitted.\n\n")
    w("Development and build-time tooling (for example `golangci-lint`, `buf`, and\n")
    w("`protoc-gen-go`, invoked via the `tool` directive in `go.mod`) is **not** included\n")
    w("here, as it is executed as a standalone tool and is neither linked into nor\n")
    w("distributed with this binary.\n\n")
    w("Attribution notices required by these licenses (Apache-2.0 §4 and the upstream\n")
    w("`NOTICE` files it references) are reproduced in the accompanying `NOTICE` file.\n\n")
    w(f"Total distributed third-party packages: {total}\n\n")
    w("## Summary\n\n")
    w("| License | Packages |\n| --- | --- |\n")
    for lic, pkgs in ordered:
        w(f"| {lic} | {len(pkgs)} |\n")
    w("\n> **MPL-2.0** is a weak, file-scoped copyleft license: the copyleft reaches only\n")
    w("> the MPL-covered files, not this project's own source. Distributing those files,\n")
    w("> even unmodified, still requires preserving their notices and license and making\n")
    w("> their source form available to recipients.\n")
    for lic, pkgs in ordered:
        w(f"\n## {lic}\n\n")
        for pkg in sorted(pkgs):
            w(f"- [{pkg}]({pkgs[pkg]})\n")
    if out is not sys.stdout:
        out.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
