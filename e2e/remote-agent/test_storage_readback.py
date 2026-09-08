import hashlib
import io
from pathlib import Path
import tempfile
import unittest

from storage_readback import find_medium, hash_bytes


class ReadbackTest(unittest.TestCase):
    def test_short_read_cannot_report_a_successful_hash(self):
        with self.assertRaisesRegex(RuntimeError, "Short USB read"):
            hash_bytes(io.BytesIO(b"abc"), 4)
        self.assertEqual(hash_bytes(io.BytesIO(b"abc-extra"), 3), hashlib.sha256(b"abc").hexdigest())

    def test_selection_requires_usb_identity_size_and_unique_whole_disk(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            blocks = root / "class"
            blocks.mkdir()
            usb = root / "usb"
            usb.mkdir()
            for field, value in {"idVendor": "1d6b", "idProduct": "0104", "serial": "test"}.items():
                (usb / field).write_text(value)
            identity = {"vendor": "1d6b", "product": "0104", "serial": "test", "size": 1024}
            def add(name, partition=False):
                disk = usb / name
                disk.mkdir()
                (disk / "size").write_text("2")
                if partition:
                    (disk / "partition").write_text("1")
                (blocks / name).symlink_to(disk)
            add("sda")
            add("sda1", partition=True)
            self.assertEqual(find_medium(blocks, identity), "sda")
            for field, value in [("vendor", "1234"), ("product", "1234"), ("serial", "other"), ("size", 512)]:
                self.assertIsNone(find_medium(blocks, {**identity, field: value}))
            add("sdb")
            with self.assertRaisesRegex(RuntimeError, "Ambiguous"):
                find_medium(blocks, identity)


if __name__ == "__main__":
    unittest.main()
