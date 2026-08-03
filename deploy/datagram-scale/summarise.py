#!/usr/bin/env python3
"""Turn one report.json per run into a delivery-versus-knob table.

  summarise.py out/sweep-n*/report.json

Rows are sorted by gateway count, then by threshold, so a sweep along either
axis reads top to bottom. Missing files are listed rather than skipped silently:
a point that never produced a report is itself a result.
"""

import json
import sys


COLUMNS = [
    ("run", 22, "s"),
    ("N", 3, "d"),
    ("P p50", 6, "d"),
    ("k", 3, "d"),
    ("f", 5, "s"),
    ("gate", 5, "d"),
    ("delivery", 9, "s"),
    ("rank p50", 9, "d"),
    ("rank max", 9, "d"),
    ("at k", 7, "s"),
    ("past gate", 10, "s"),
    ("senders", 8, "s"),
    ("recoders", 9, "d"),
]


def load(path):
    with open(path, encoding="utf-8") as fh:
        d = json.load(fh)
    d["_path"] = path
    return d


def main(paths):
    rows, missing = [], []
    for path in paths:
        try:
            rows.append(load(path))
        except (OSError, json.JSONDecodeError) as err:
            missing.append(f"{path}: {err}")

    if not rows:
        print("no reports to summarise")
        for m in missing:
            print(f"  missing {m}")
        return 1

    rows.sort(key=lambda d: (d.get("gateways", 0), d.get("f", 0) or 0))
    print("  ".join(f"{name:<{width}}" for name, width, _ in COLUMNS))
    for d in rows:
        name = d["_path"].split("/")[-2]
        frac = d.get("delivery_fraction")
        cells = [
            f"{name[:22]:<22}",
            f"{d.get('gateways', 0):<3d}",
            f"{d.get('peers_p50', 0):<6d}",
            f"{d.get('k', 0):<3d}",
            f"{round(float(d.get('f', 0)), 4):<5}",
            f"{d.get('forward_gate', 0):<5d}",
            f"{'n/a' if frac is None else f'{100.0 * frac:.1f}%':<9}",
            f"{d.get('rank_p50', 0):<9d}",
            f"{d.get('rank_max', 0):<9d}",
            f"{100.0 * d.get('rank_reached_k_fraction', 0):.0f}%".ljust(7),
            f"{100.0 * d.get('rank_above_gate_fraction', 0):.0f}%".ljust(10),
            f"{d.get('senders', 0)}/{d.get('gateways', 0)}".ljust(8),
            f"{d.get('recoders', 0):<9d}",
        ]
        suffix = "" if d.get("preconditions_ok", True) else "   (preconditions failed)"
        print("  ".join(cells) + suffix)

    for m in missing:
        print(f"missing {m}")
    return 0


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    sys.exit(main(sys.argv[1:]))
