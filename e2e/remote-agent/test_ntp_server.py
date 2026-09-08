import struct
import unittest
from ntp_server import response


class NtpResponderTest(unittest.TestCase):
    def test_response_echoes_origin_and_encodes_current_time(self):
        request = bytearray(48)
        request[0] = 0x23
        request[40:48] = b"request1"
        packet = response(request, 1800000000.5)
        self.assertEqual(len(packet), 48)
        self.assertEqual(packet[0] & 7, 4)
        self.assertEqual(packet[1], 2)
        self.assertEqual(packet[24:32], request[40:48])
        self.assertEqual(struct.unpack("!II", packet[40:48]), (4008988800, 1 << 31))

    def test_malformed_and_nonclient_packets_are_ignored(self):
        self.assertIsNone(response(b"short", 0))
        self.assertIsNone(response(bytes(48), 0))


if __name__ == "__main__":
    unittest.main()
