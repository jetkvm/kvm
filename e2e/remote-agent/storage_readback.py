"""Read-only SHA-256 verification of one explicitly identified USB medium."""

import base64
import hashlib
import json
from pathlib import Path
import re
import stat
import sys
import time


def find_medium(root, identity):
    matches = []
    for block in root.iterdir():
        if (block / "partition").exists():
            continue
        for parent in block.resolve().parents:
            if not (parent / "idVendor").exists():
                continue
            vendor = (parent / "idVendor").read_text().strip().lower()
            product = (parent / "idProduct").read_text().strip().lower()
            serial = (parent / "serial").read_text().strip() if (parent / "serial").exists() else ""
            if (vendor == identity["vendor"] and product == identity["product"]
                    and (not identity["serial"] or serial == identity["serial"])
                    and int((block / "size").read_text()) * 512 == identity["size"]):
                matches.append(block.name)
            break
    if len(matches) > 1:
        raise RuntimeError(f"Ambiguous USB medium: {matches}")
    return matches[0] if matches else None


def hash_bytes(source, size):
    digest = hashlib.sha256()
    remaining = size
    while remaining:
        chunk = source.read(min(1024 * 1024, remaining))
        if not chunk:
            raise RuntimeError(f"Short USB read: {remaining} bytes missing")
        digest.update(chunk)
        remaining -= len(chunk)
    return digest.hexdigest()


def main():
    identity = json.loads(base64.b64decode(sys.argv[1], validate=True))
    for field in ("vendor", "product"):
        if not re.fullmatch(r"[0-9a-f]{4}", identity[field]):
            raise ValueError(f"Invalid {field}")
    if not isinstance(identity["size"], int) or not 0 < identity["size"] <= 1024**3:
        raise ValueError("Readback must be between 1 byte and 1 GiB")
    deadline = time.monotonic() + 25
    while True:
        name = find_medium(Path("/sys/class/block"), identity)
        if name:
            break
        if time.monotonic() >= deadline:
            raise RuntimeError("Expected USB medium did not enumerate")
        time.sleep(0.25)
    device = Path("/dev") / name
    if not stat.S_ISBLK(device.stat().st_mode):
        raise RuntimeError("Selected path is not a block device")
    with device.open("rb", buffering=0) as source:
        result = hash_bytes(source, identity["size"])
    print(json.dumps({"device": str(device), "bytes": identity["size"], "sha256": result}))


if __name__ == "__main__":
    main()
