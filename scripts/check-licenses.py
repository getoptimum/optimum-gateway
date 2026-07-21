#!/usr/bin/env python3
"""
check-licenses.py: validate go-licenses CSV output against a policy file.

Usage:
    go-licenses csv github.com/getoptimum/optimum-gateway/cmd | python3 scripts/check-licenses.py
    go-licenses csv ./cmd | python3 scripts/check-licenses.py --config licenses.yaml --strict-unknown
    go-licenses csv ./... | python3 scripts/check-licenses.py --config licenses.yaml --report-only
    python3 scripts/check-licenses.py --config licenses.yaml licenses.csv

Exit codes:
    0  All packages pass policy (always 0 when --report-only)
    1  One or more policy violations found
    2  Configuration error (missing/malformed policy file)
"""

import argparse
import csv
import io
import os
import sys

# ---------------------------------------------------------------------------
# Minimal YAML parser (stdlib only, no pip install needed)
# Handles the specific schema used in licenses.yaml.
# ---------------------------------------------------------------------------

def _dequote(value):
    """Strip surrounding quotes and whitespace from a scalar value.

    PyYAML unquotes scalars; the fallback parser must match so a quoted
    exception `license: "GPL-3.0-only"` compares equal to the detected id.
    """
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ('"', "'"):
        value = value[1:-1]
    return value


def _parse_yaml(text):
    """Parse a subset of YAML sufficient for licenses.yaml into a dict.

    Supported constructs:
      - Top-level keys with dict values
      - Nested keys with list values (items prefixed with '  - ')
      - Comment lines (starting with optional whitespace + '#')
      - Inline comments after values (stripped)
    """
    result = {}
    current_top = None
    current_sub = None
    current_list_item = None

    for raw_line in text.splitlines():
        # Strip inline comments and trailing whitespace
        line = raw_line.split('#')[0].rstrip()

        if not line.strip():
            continue

        indent = len(line) - len(line.lstrip())

        if indent == 0:
            # Top-level key
            if line.endswith(':'):
                current_top = line[:-1].strip()
                result[current_top] = {}
                current_sub = None
                current_list_item = None
        elif indent == 2 and current_top is not None:
            stripped = line.strip()
            if stripped.startswith('- '):
                # List item directly under top-level key
                if not isinstance(result.get(current_top), list):
                    result[current_top] = []
                value = stripped[2:].strip()
                if ':' in value:
                    k, _, v = value.partition(':')
                    current_list_item = {k.strip(): _dequote(v)}
                    result[current_top].append(current_list_item)
                    current_sub = None
                else:
                    result[current_top].append(_dequote(value))
                    current_list_item = None
                    current_sub = None
            elif stripped.endswith(':'):
                # Sub-key under top-level dict
                current_sub = stripped[:-1]
                current_list_item = None
                if isinstance(result[current_top], dict):
                    result[current_top][current_sub] = []
        elif indent == 4 and current_top is not None:
            stripped = line.strip()
            if current_sub is not None and stripped.startswith('- '):
                value = stripped[2:].strip()
                target = result[current_top]
                if isinstance(target, dict) and isinstance(target.get(current_sub), list):
                    target[current_sub].append(_dequote(value))
            elif current_list_item is not None and ':' in stripped:
                k, _, v = stripped.partition(':')
                current_list_item[k.strip()] = _dequote(v)

    return result


def load_config(path):
    """Load and validate licenses.yaml, return (blocked_set, exceptions_dict).

    Falls back to the minimal embedded YAML parser when PyYAML is unavailable.
    """
    if not os.path.isfile(path):
        print(f"ERROR: config file not found: {path}", file=sys.stderr)
        sys.exit(2)

    with open(path, encoding="utf-8") as fh:
        text = fh.read()

    try:
        import yaml  # type: ignore
        data = yaml.safe_load(text)
    except ImportError:
        data = _parse_yaml(text)
    except yaml.YAMLError as exc:
        print(f"ERROR: failed to parse {path}: {exc}", file=sys.stderr)
        sys.exit(2)

    policy = data.get("policy", {})
    blocked = set(policy.get("blocked_licenses", []))
    if not blocked:
        print(f"WARNING: no blocked_licenses defined in {path}", file=sys.stderr)

    raw_exceptions = data.get("exceptions", []) or []
    exceptions = {}
    if isinstance(raw_exceptions, list):
        for entry in raw_exceptions:
            if isinstance(entry, dict) and "package" in entry:
                exceptions[entry["package"]] = entry

    return blocked, exceptions


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def _license_blocked(lic, blocked):
    """True if a detected license is blocked.

    Matches exact SPDX ids and also compound expressions or variant forms that
    embed a blocked id (e.g. "MIT AND GPL-3.0-only", "GPL-3.0+"), so copyleft
    cannot slip through inside an expression. Biased to fail closed.
    """
    return lic in blocked or any(b in lic for b in blocked)


def main():
    parser = argparse.ArgumentParser(
        description="Validate go-licenses CSV against a license policy file."
    )
    parser.add_argument(
        "--config",
        default="licenses.yaml",
        help="Path to licenses.yaml policy file (default: licenses.yaml)",
    )
    parser.add_argument(
        "--strict-unknown",
        action="store_true",
        help="Treat packages with unknown/missing licenses as violations",
    )
    parser.add_argument(
        "--report-only",
        action="store_true",
        help="Print the inventory and any violations but always exit 0 "
             "(for non-shipped test/infra deps that must not gate the build)",
    )
    parser.add_argument(
        "csv_file",
        nargs="?",
        help="CSV file from go-licenses (default: read from stdin)",
    )
    args = parser.parse_args()

    blocked, exceptions = load_config(args.config)

    # Read CSV input
    if args.csv_file:
        try:
            with open(args.csv_file, encoding="utf-8") as fh:
                raw = fh.read()
        except OSError as exc:
            print(f"ERROR: cannot read CSV file: {exc}", file=sys.stderr)
            sys.exit(2)
    else:
        raw = sys.stdin.read()

    if not raw.strip():
        print("ERROR: empty input, go-licenses produced no output", file=sys.stderr)
        if args.report_only:
            print("RESULT: no input (report-only, not failing)")
            sys.exit(0)
        print("RESULT: FAILED (empty input)")
        sys.exit(1)

    reader = csv.reader(io.StringIO(raw))

    violations = []
    warnings = []
    excepted = []
    passed = []

    for row in reader:
        if len(row) < 3:
            # Surface malformed rows rather than silently dropping a package.
            if any(field.strip() for field in row):
                print(f"WARNING: skipping malformed CSV row: {row}", file=sys.stderr)
            continue
        pkg, url, lic = row[0].strip(), row[1].strip(), row[2].strip()

        if lic in ("", "Unknown"):
            # A documented exception clears an undetected license (go-licenses
            # detection gaps for permissively-licensed deps), but only if the
            # exception itself declares a non-blocked license.
            exc_entry = exceptions.get(pkg)
            if exc_entry and not _license_blocked(exc_entry.get("license", "").strip(), blocked):
                review = exc_entry.get("review_date", "unknown")
                excepted.append((pkg, url, lic or "Unknown", "", review))
            elif exc_entry:
                violations.append((pkg, url, lic or "Unknown",
                                   "exception declares a blocked license"))
            elif args.strict_unknown:
                violations.append((pkg, url, lic, "unknown license (--strict-unknown)"))
            else:
                warnings.append((pkg, url, lic))
        elif _license_blocked(lic, blocked):
            # Only clear a blocked license when the exception explicitly declares
            # that same license. An exception filed for a detection gap (declared
            # as a permissive license) must not silently pass a package that later
            # actually resolves to blocked copyleft.
            exc_entry = exceptions.get(pkg)
            if exc_entry and exc_entry.get("license", "").strip() == lic:
                review = exc_entry.get("review_date", "unknown")
                excepted.append((pkg, url, lic, "", review))
            else:
                violations.append((pkg, url, lic, "blocked license"))
        else:
            passed.append((pkg, url, lic))

    # Print results
    for pkg, url, lic in passed:
        print(f"  OK       {lic:<20} {pkg}")

    for pkg, url, lic, reason, review in excepted:
        print(f"  EXCEPTED {lic:<20} {pkg}  (reviewed: {review})")

    for pkg, url, lic in warnings:
        print(f"  WARNING  {'Unknown':<20} {pkg}  (license not detected)")

    for pkg, url, lic, reason in violations:
        print(f"  BLOCKED  {lic:<20} {pkg}  ({reason})")

    print()
    total = len(passed) + len(excepted) + len(warnings) + len(violations)
    print(f"Checked {total} package(s): "
          f"{len(passed)} OK, {len(excepted)} excepted, "
          f"{len(warnings)} warnings, {len(violations)} violations")

    if args.report_only:
        print()
        print("RESULT: REPORT-ONLY (non-shipped deps, not gating the build)")
        sys.exit(0)

    if violations:
        print()
        print("RESULT: FAILED (add exceptions to licenses.yaml or remove the dependency)")
        sys.exit(1)

    print("RESULT: PASSED")
    sys.exit(0)


if __name__ == "__main__":
    main()
