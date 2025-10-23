#!/usr/bin/env python3
"""Find excess translation keys that exist in other language files but are not in en.json

Usage:
    python3 tools/find_excess_messages.py

Optional: set `--json` to print machine-readable JSON output.
Optional: set `--path` to point to the directory containing messages JSON files.
Optional: set `--out` to specify a file to write the JSON report to.
"""
import argparse
import json
import sys
from pathlib import Path
from json import JSONDecodeError

def load_json(path: Path):
    try:
        return json.loads(path.read_text(encoding='utf-8'))
    except (JSONDecodeError, OSError) as e:
        print(f"Failed to read {path}: {e}")
        return {}

def main(argv):
    p = argparse.ArgumentParser(
        description="Sort translations keys in message *.json files"
    )
    p.add_argument("--path", default="./localization/messages/", help="path to messages *.json")
    p.add_argument("--json", default=False, action="store_true", help="output excess keys as JSON")
    p.add_argument("--out", default="./reports/excess_messages.json", help="output report file")
    args = p.parse_args()

    messages_path = Path(args.path)
    if not messages_path.is_dir():
        print(f"message path is not a directory: {messages_path}")
        raise SystemExit(2)

    files = list(messages_path.glob("*.json"))
    if len(files) == 0:
        print(f"no message files (*.json) found in: {messages_path}")
        raise SystemExit(3)

    en_path = messages_path / 'en.json'
    en = load_json(en_path) if en_path.exists() else {}
    en_keys = set(en.keys())

    extras = {}  # key -> list of files where present

    for f in files:
        if f.name == 'en.json':
            continue
        data = load_json(f)
        for k in data.keys():
            if k not in en_keys:
                extras.setdefault(k, []).append(f.name)

    if '--json' in argv:
        print(json.dumps(extras, ensure_ascii=False, indent=2))
        return 0

    if not extras:
        print('No excess message keys found in other languages.')
        return 0

    print(f"Message keys present in other languages but not in en.json ({len(extras)}):\n")
    for key, files in sorted(extras.items()):
        print(f"{key}: {', '.join(files)}")

    return 0

if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
