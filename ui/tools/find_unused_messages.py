#!/usr/bin/env python3
import argparse
import json
import os
import re
from datetime import datetime
from pathlib import Path

def flatten(d, prefix=""):
    for k, v in d.items():
        key = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict):
            yield from flatten(v, key)
        else:
            yield key

def gather_files(
    src_dir, exts=(".ts", ".tsx", ".js", ".jsx")
):
    for root, _, files in os.walk(src_dir):
        parts = root.split(os.sep)
        if "node_modules" in parts or ".git" in parts:
            continue
        for fn in files:
            if fn.endswith(exts):
                yield Path(root) / fn

def find_usages(keys, files):
    usages = {k: [] for k in keys}
    
    print(f"Compiling {len(keys)} patterns for keys ...")
    # Precompile patterns for speed
    patterns = {k: re.compile(r"\bm\." + re.escape(k) + r"\s*\(") for k in keys}

    print(f"Scanning files...")
    for file in files:
        try:
            text = file.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            # skip files that can't be read due to I/O or decoding errors
            continue
        lines = text.splitlines()
        for i, line in enumerate(lines, start=1):
            for k, pat in patterns.items():
                if pat.search(line):
                    usages[k].append(
                        {"file": str(file), "line": i, "text": line.strip()}
                    )
    return usages

def main():
    p = argparse.ArgumentParser(
        description="Generate JSON report of localization key usage (m.key_name_here())."
    )
    p.add_argument(
        "--en", default="./localization/messages/en.json", help="path to en.json"
    )
    p.add_argument("--src", default="./", help="root source directory to scan")
    p.add_argument(
        "--out", default="./reports/localization_key_usage.json", help="output report file"
    )
    args = p.parse_args()

    en_path = Path(args.en)
    if not en_path.is_file():
        print(f"en.json not found: {en_path}", flush=True)
        raise SystemExit(2)

    print(f"Reading english from {en_path}")
    with en_path.open(encoding="utf-8") as f:
        payload = json.load(f)

    keys = sorted(list(flatten(payload)))
    keys.pop(0) if keys and keys[0] == "$schema" else None  # remove $schema if present
    print(f"Found {len(keys)} localization keys")
    
    print(f"Gathering source files in {args.src} ...")
    files = list(gather_files(args.src))
    
    print(f"Scanning {len(files)} source files ...")
    usages = find_usages(keys, files)

    print(f"Generating report for {len(usages)} usages ...")
    report = {
        "generated_at": datetime.utcnow().isoformat() + "Z",
        "en_json": str(en_path),
        "src_root": args.src,
        "total_keys": len(keys),
        "keys": {},
    }

    for k in keys:
        occ = usages.get(k, [])
        used = bool(occ)
        report["keys"][k] = {"used": used, "occurrences": occ}

    unused_keys = [k for k, v in report["keys"].items() if not v["used"]]
    unused_count = len(unused_keys)
    print(f"Found {unused_count} unused keys")

    report["unused_count"] = unused_count
    report["unused_keys"] = unused_keys

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as f:
        json.dump(report, f, indent=2, ensure_ascii=False)

    print(f"Report written to {out_path}")
    print(f"Total keys: {report['total_keys']}, Unused: {report['unused_count']}")


if __name__ == "__main__":
    main()
