#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

def main():
    p = argparse.ArgumentParser(
        description="Sort translations keys in message *.json files"
    )
    p.add_argument(
        "--path", default="./localization/messages/", help="path to messages *.json"
    )
    args = p.parse_args()

    messages_path = Path(args.path)
    if not messages_path.is_dir():
        print(f"message path is not a directory: {messages_path}")
        raise SystemExit(2)

    files = list(messages_path.glob("*.json"))
    if len(files) == 0:
        print(f"no message files (*.json) found in: {messages_path}")
        raise SystemExit(3)

    for f in files:
        print(f"Processing {f.name} ...")
        data = json.loads(f.read_text(encoding="utf-8"))

        # Keep $schema first if present
        schema = None
        if "$schema" in data:
            schema = data.pop("$schema")

        sorted_items = dict(sorted(data.items()))

        if schema is not None:
            out = {"$schema": schema}
            out.update(sorted_items)
        else:
            out = sorted_items

        f.write_text(
            json.dumps(out, ensure_ascii=False, indent=4) + "\n", encoding="utf-8"
        )

    print(f"Processed {len(files)} files in {messages_path}")

if __name__ == "__main__":
    main()
