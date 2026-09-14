import base64
import errno
import hashlib
import io
import json
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import DEFAULT, patch

from storage_readback import find_medium, hash_bytes, main


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


class ReadbackPollingTest(unittest.TestCase):
    def setUp(self):
        identity = {"vendor": "1d6b", "product": "0104", "serial": "test", "size": 3}
        argument = base64.b64encode(json.dumps(identity).encode()).decode()
        self.mock("sys.argv", ["storage_readback.py", argument])
        self.output = self.mock("sys.stdout", io.StringIO())
        self.find = self.mock("storage_readback.find_medium", return_value="sda")
        self.stat = self.mock("storage_readback.Path.stat",
                              return_value=SimpleNamespace(st_mode=stat.S_IFBLK))
        self.source = io.BytesIO(b"abc")
        self.open = self.mock("storage_readback.Path.open", return_value=self.source)
        self.clock = self.mock("storage_readback.time.monotonic", return_value=0)
        self.sleep = self.mock("storage_readback.time.sleep")

    def mock(self, *args, **kwargs):
        patcher = patch(*args, **kwargs)
        result = patcher.start()
        self.addCleanup(patcher.stop)
        return result

    def test_transient_discovery_stat_and_open_errors_are_retried(self):
        for name, stage in (("find", self.find), ("stat", self.stat), ("open", self.open)):
            for code in (errno.ENOENT, errno.ENODEV, errno.ENXIO):
                with self.subTest(stage=name, errno=code):
                    source = io.BytesIO(b"abc")
                    self.open.return_value = source
                    self.output.seek(0)
                    self.output.truncate()
                    self.sleep.reset_mock()
                    stage.side_effect = [OSError(code, "not ready"), DEFAULT]
                    try:
                        main()
                    finally:
                        stage.side_effect = None
                    self.sleep.assert_called_once_with(0.25)
                    self.assertEqual(json.loads(self.output.getvalue()), {
                        "device": "/dev/sda", "bytes": 3,
                        "sha256": hashlib.sha256(b"abc").hexdigest(),
                    })
                    self.assertTrue(source.closed)

    def test_missing_or_disappearing_medium_still_times_out(self):
        for error in (None, FileNotFoundError(errno.ENOENT, "disappeared")):
            with self.subTest(error=error):
                self.find.return_value = None
                self.find.side_effect = error
                self.clock.side_effect = [0, 24, 25]
                self.sleep.reset_mock()
                with self.assertRaisesRegex(RuntimeError, "did not enumerate"):
                    main()
                self.sleep.assert_called_once_with(0.25)
                self.open.assert_not_called()
                self.assertEqual(self.output.getvalue(), "")

    def test_permanent_os_errors_fail_without_retry(self):
        for name, stage in (("find", self.find), ("stat", self.stat), ("open", self.open)):
            for code in (errno.EACCES, errno.EPERM, errno.EIO):
                with self.subTest(stage=name, errno=code):
                    stage.side_effect = OSError(code, "permanent")
                    try:
                        with self.assertRaises(OSError) as raised:
                            main()
                    finally:
                        stage.side_effect = None
                    self.assertEqual(raised.exception.errno, code)
                    self.sleep.assert_not_called()
                    self.assertEqual(self.output.getvalue(), "")

    def test_ambiguous_and_non_block_devices_fail_without_retry(self):
        self.find.side_effect = RuntimeError("Ambiguous USB medium")
        with self.assertRaisesRegex(RuntimeError, "Ambiguous"):
            main()
        self.find.side_effect = None
        self.stat.return_value.st_mode = stat.S_IFREG
        with self.assertRaisesRegex(RuntimeError, "not a block device"):
            main()
        self.sleep.assert_not_called()
        self.open.assert_not_called()
        self.assertEqual(self.output.getvalue(), "")

    def test_short_read_fails_without_retry_and_closes_device(self):
        self.source.truncate(2)
        with self.assertRaisesRegex(RuntimeError, "Short USB read"):
            main()
        self.sleep.assert_not_called()
        self.assertTrue(self.source.closed)
        self.assertEqual(self.output.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
