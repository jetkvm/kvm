"""Bounded test NTP responder. Exit when the owning SSH stdin closes."""
import json
import socket
import struct
import sys
import threading
import time


def response(request, now):
    if len(request) < 48 or request[0] & 7 != 3:
        return None
    packet = bytearray(48)
    packet[0:4] = bytes([0x24, 2, 6, 0xEC])
    packet[12:16] = b"TEST"
    seconds = int(now) + 2208988800
    stamp = struct.pack("!II", seconds & 0xFFFFFFFF, int((now % 1) * (1 << 32)))
    packet[16:24] = stamp
    packet[24:32] = request[40:48]
    packet[32:40] = stamp
    packet[40:48] = stamp
    return bytes(packet)


def main():
    source = sys.argv[1]
    stop = threading.Event()
    def owner():
        sys.stdin.buffer.read()
        stop.set()
    threading.Thread(target=owner, daemon=True).start()
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.bind(("0.0.0.0", 123))
        sock.settimeout(0.2)
        print(json.dumps({"ready": True}), flush=True)
        deadline = time.monotonic() + 300
        while not stop.is_set() and time.monotonic() < deadline:
            try:
                data, addr = sock.recvfrom(512)
            except socket.timeout:
                continue
            if addr[0] != source:
                continue
            packet = response(data, time.time())
            if packet:
                sock.sendto(packet, addr)
                print(json.dumps({"request": True}), flush=True)


if __name__ == "__main__":
    main()
