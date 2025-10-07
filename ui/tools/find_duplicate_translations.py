#!/usr/bin/env python3
import argparse
import json
import re
from datetime import datetime
from pathlib import Path
import sys

def flatten_strings(obj, prefix=""):
    if isinstance(obj, dict):
        for k, v in obj.items():
            key = f"{prefix}.{k}" if prefix else k
            yield from flatten_strings(v, key)
    else:
        # only consider scalar strings for translation targets
        if isinstance(obj, str):
            yield prefix, obj

def normalize(s, ignore_case=False, trim=False, collapse_ws=False):
    if collapse_ws:
        s = re.sub(r"\s+", " ", s)
    if trim:
        s = s.strip()
    if ignore_case:
        s = s.lower()
    return s

def main(argv):
    p = argparse.ArgumentParser(
        description="Find identical translation targets with different keys in en.json"
    )
    p.add_argument(
        "--en", default="./localization/messages/en.json", help="path to en.json"
    )
    p.add_argument(
        "--out",
        default="./reports/duplicate_translation_targets.json",
        help="output report path (JSON)",
    )
    p.add_argument(
        "--ignore-case", default=True, action="store_true", help="ignore case when comparing values"
    )
    p.add_argument(
        "--trim",
        default=True, action="store_true",
        help="trim surrounding whitespace before comparing",
    )
    p.add_argument(
        "--collapse-ws",
        default=True, action="store_true",
        help="collapse internal whitespace before comparing",
    )
    args = p.parse_args()

    en_path = Path(args.en)
    if not en_path.is_file():
        print(f"en.json not found: {en_path}")
        raise SystemExit(2)

    with en_path.open(encoding="utf-8") as f:
        payload = json.load(f)

    entries = list(flatten_strings(payload))
    total_keys = len(entries)

    groups = {}
    original_values = {}
    for key, val in entries:
        norm = normalize(
            val,
            ignore_case=args.ignore_case,
            trim=args.trim,
            collapse_ws=args.collapse_ws,
        )
        groups.setdefault(norm, []).append(key)
        # keep the first seen original for reporting
        original_values.setdefault(norm, val)

    duplicates = []
    for norm, keys in groups.items():
        if len(keys) > 1:
            duplicates.append(
                {
                    "normalized_value": norm,
                    "original_value": original_values.get(norm),
                    "keys": sorted(keys),
                    "count": len(keys),
                }
            )

    report = {
        "generated_at": datetime.utcnow().isoformat() + "Z",
        "en_json": str(en_path),
        "total_string_keys": total_keys,
        "duplicate_groups": sorted(
            duplicates, key=lambda d: (-d["count"], d["normalized_value"])
        ),
        "duplicate_count": len(duplicates),
    }

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as f:
        json.dump(report, f, indent=2, ensure_ascii=False)

    print(
        f"Wrote {out_path} — total keys: {total_keys}, duplicate groups: {len(duplicates)}"
    )

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
