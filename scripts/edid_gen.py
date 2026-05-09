#!/usr/bin/env python3
"""
EDID generator for JetKVM high-refresh-rate testing.

Generates EDIDs with custom CVT-RB detailed timings, suitable for advertising
non-standard 480p/720p high-refresh modes to the source PC.

Outputs hex strings ready for the JetKVM `setEDID` JSONRPC.
"""

import math
import sys

# CVT Reduced Blanking v1 constants (VESA CVT 1.0)
RB_H_BLANK = 160
RB_H_SYNC = 32
RB_H_BACK_PORCH = 80
RB_H_FRONT_PORCH = RB_H_BLANK - RB_H_SYNC - RB_H_BACK_PORCH  # 48
RB_V_FRONT_PORCH = 3
RB_V_BACK_PORCH = 6
RB_MIN_V_BLANK_US = 460
CLOCK_STEP_KHZ = 250


def _v_sync_for_aspect(h, v):
    ratio = h / v
    # VESA aspect-dependent vsync widths
    if abs(ratio - 4 / 3) < 0.05:
        return 4
    if abs(ratio - 16 / 9) < 0.05:
        return 5
    if abs(ratio - 16 / 10) < 0.05:
        return 6
    if abs(ratio - 5 / 4) < 0.05:
        return 7
    if abs(ratio - 15 / 9) < 0.05:
        return 7
    return 10  # custom


def cvt_rb(h_active, v_active, refresh):
    """Compute CVT-RB v1 timings. Returns dict with full timing breakdown."""
    h_active = (h_active // 8) * 8  # round down to multiple of 8

    h_period_est_us = (1_000_000 / refresh - RB_MIN_V_BLANK_US) / v_active
    v_blank_lines = math.ceil(RB_MIN_V_BLANK_US / h_period_est_us)
    if v_blank_lines < 14:
        v_blank_lines = 14  # absolute floor for RB

    v_total = v_active + v_blank_lines
    v_sync = _v_sync_for_aspect(h_active, v_active)
    # CVT-RB v1: vertical front porch is fixed at RB_V_FRONT_PORCH (3),
    # vertical sync is aspect-dependent, vertical back porch absorbs the
    # remainder of v_blank.
    v_front = RB_V_FRONT_PORCH
    v_back = v_blank_lines - v_front - v_sync
    if v_back < RB_V_BACK_PORCH:
        # Bump v_blank so back porch meets the spec minimum.
        v_blank_lines += RB_V_BACK_PORCH - v_back
        v_back = RB_V_BACK_PORCH
        v_total = v_active + v_blank_lines

    h_total = h_active + RB_H_BLANK
    raw_pclk_hz = v_total * h_total * refresh
    # Quantize to CLOCK_STEP (250 kHz) per CVT spec.
    pclk_hz = (raw_pclk_hz // (CLOCK_STEP_KHZ * 1000)) * CLOCK_STEP_KHZ * 1000
    if pclk_hz == 0:
        # Refresh rate so low that quantization snapped to zero — fall back
        # to the raw value.
        pclk_hz = raw_pclk_hz

    actual_refresh = pclk_hz / (v_total * h_total)

    return {
        "h_active": h_active,
        "h_blank": RB_H_BLANK,
        "h_front": RB_H_FRONT_PORCH,
        "h_sync": RB_H_SYNC,
        "v_active": v_active,
        "v_blank": v_blank_lines,
        "v_front": v_front,
        "v_sync": v_sync,
        "h_total": h_total,
        "v_total": v_total,
        "pclk_hz": pclk_hz,
        "refresh": actual_refresh,
        # CVT-RB always has positive Hsync, negative Vsync
        "hsync_pos": True,
        "vsync_pos": False,
    }


def make_dtd(t):
    """Build an 18-byte detailed timing descriptor from cvt_rb() output."""
    pclk_10khz = t["pclk_hz"] // 10_000
    if pclk_10khz > 0xFFFF:
        raise ValueError("pixel clock too high for DTD")
    h_act = t["h_active"]
    h_bl = t["h_blank"]
    v_act = t["v_active"]
    v_bl = t["v_blank"]
    h_off = t["h_front"]
    h_pw = t["h_sync"]
    v_off = t["v_front"]
    v_pw = t["v_sync"]
    # Image size in mm; pick a 16:9-ish ~24" display
    h_mm = 530
    v_mm = 300
    h_border = 0
    v_border = 0

    # Flags byte:
    # bit 7-5: stereo (0)
    # bit 4-3: digital separate sync (11)
    # bit 2: vsync polarity (1=pos, 0=neg)
    # bit 1: hsync polarity (1=pos, 0=neg)
    # bit 0: 0
    flags = 0b00011000  # digital separate
    if t["hsync_pos"]:
        flags |= 0b00000010
    if t["vsync_pos"]:
        flags |= 0b00000100

    dtd = bytearray(18)
    dtd[0] = pclk_10khz & 0xFF
    dtd[1] = (pclk_10khz >> 8) & 0xFF
    dtd[2] = h_act & 0xFF
    dtd[3] = h_bl & 0xFF
    dtd[4] = ((h_act >> 8) & 0x0F) << 4 | ((h_bl >> 8) & 0x0F)
    dtd[5] = v_act & 0xFF
    dtd[6] = v_bl & 0xFF
    dtd[7] = ((v_act >> 8) & 0x0F) << 4 | ((v_bl >> 8) & 0x0F)
    dtd[8] = h_off & 0xFF
    dtd[9] = h_pw & 0xFF
    dtd[10] = ((v_off & 0x0F) << 4) | (v_pw & 0x0F)
    dtd[11] = (
        ((h_off >> 8) & 0x03) << 6
        | ((h_pw >> 8) & 0x03) << 4
        | ((v_off >> 4) & 0x03) << 2
        | ((v_pw >> 4) & 0x03)
    )
    dtd[12] = h_mm & 0xFF
    dtd[13] = v_mm & 0xFF
    dtd[14] = ((h_mm >> 8) & 0x0F) << 4 | ((v_mm >> 8) & 0x0F)
    dtd[15] = h_border
    dtd[16] = v_border
    dtd[17] = flags
    return bytes(dtd)


def make_range_descriptor(vmin, vmax, hmin_khz, hmax_khz, max_pclk_mhz):
    """
    EDID 1.4 monitor range descriptor.
    Supports rates >=255 Hz via the offset byte (byte 4).
    """
    # Byte 4 'offset' encoding (EDID 1.4):
    #   bit 0: V min rate offset (0 or 255)
    #   bit 1: V max rate offset (0 or 255)
    #   bit 2: H min rate offset (0 or 255)
    #   bit 3: H max rate offset (0 or 255)
    # If we need V or H over 255, set offset bit and store value-255.
    offset_byte = 0
    vmin_b, vmax_b = vmin, vmax
    hmin_b, hmax_b = hmin_khz, hmax_khz
    if vmax > 255:
        offset_byte |= 0b10
        vmax_b = vmax - 255
    if vmin > 255:
        offset_byte |= 0b01
        vmin_b = vmin - 255
    if hmax_khz > 255:
        offset_byte |= 0b1000
        hmax_b = hmax_khz - 255
    if hmin_khz > 255:
        offset_byte |= 0b0100
        hmin_b = hmin_khz - 255

    desc = bytearray(18)
    desc[0:4] = b"\x00\x00\x00\xfd"
    desc[4] = offset_byte
    desc[5] = vmin_b & 0xFF
    desc[6] = vmax_b & 0xFF
    desc[7] = hmin_b & 0xFF
    desc[8] = hmax_b & 0xFF
    desc[9] = max_pclk_mhz // 10
    desc[10] = 0x00  # default GTF
    desc[11:18] = b"\x0a\x20\x20\x20\x20\x20\x20"
    return bytes(desc)


def make_name_descriptor(name):
    desc = bytearray(18)
    desc[0:4] = b"\x00\x00\x00\xfc"
    desc[4] = 0x00
    name_b = name.encode("ascii")[:13]
    desc[5 : 5 + len(name_b)] = name_b
    if len(name_b) < 13:
        desc[5 + len(name_b)] = 0x0A  # LF terminator
        for i in range(5 + len(name_b) + 1, 18):
            desc[i] = 0x20
    return bytes(desc)


def make_dummy_descriptor():
    desc = bytearray(18)
    desc[0:4] = b"\x00\x00\x00\x10"
    return bytes(desc)


def make_base_block(modes, monitor_name="JetKVM HiHz", vmin=23, vmax=250, hmin=15, hmax=255, max_pclk_mhz=160):
    """
    Build a 128-byte EDID 1.3/1.4 base block.

    modes: list of cvt_rb() outputs. The first one becomes the preferred DTD.
    Up to 2 DTDs in base block (slots 1 & 2). The other two slots get range
    descriptor and name descriptor.
    """
    if len(modes) < 1:
        raise ValueError("need at least one mode")

    edid = bytearray(128)
    # Header
    edid[0:8] = b"\x00\xff\xff\xff\xff\xff\xff\x00"
    # Manufacturer ID "JET" -> 3 5-bit chars: J=10, E=5, T=20
    # JetKVM uses 0x28 0xB4 = 'JKV' originally in stock; we keep that.
    edid[8] = 0x28
    edid[9] = 0xB4
    # Product code
    edid[10] = 0x02
    edid[11] = 0x00
    # Serial number (32-bit LE)
    edid[12:16] = b"\x01\xee\xff\xc0"
    # Week / Year (year of manufacture: 2024 = 0x22 = 34)
    edid[16] = 0x30
    edid[17] = 0x23
    # EDID version 1.4 (so extended range descriptors are accepted by modern GPUs)
    edid[18] = 0x01
    edid[19] = 0x04
    # Video input definition: digital, 8 bpc, HDMI
    edid[20] = 0x80
    # Max horizontal/vertical image size in cm
    edid[21] = 0x35
    edid[22] = 0x1E
    # Display gamma (2.2)
    edid[23] = 0x78
    # Feature support: digital, YCbCr 4:2:2 + RGB 4:4:4, sRGB color space,
    # preferred timing in DTD0 (mandatory in EDID 1.4)
    edid[24] = 0x0E
    # Chromaticity / color characteristics (sRGB)
    edid[25:35] = b"\xee\x91\xa3\x54\x4c\x99\x26\x0f\x50\x54"
    # Established timings: bit 5 = 640x480@60 (recommended by EDID spec for
    # VGA-fallback compatibility). Source still prefers DTD0.
    edid[35] = 0x20
    edid[36] = 0x00
    edid[37] = 0x00
    # Standard timings (8 of them; all unused = 0x01 0x01)
    for i in range(38, 54):
        edid[i] = 0x01

    # 4 x 18-byte descriptors at bytes 54-125
    # We give: DTD0 = preferred mode, DTD1 = second mode (if any), then range, then name
    descs = []
    descs.append(make_dtd(modes[0]))
    if len(modes) >= 2:
        descs.append(make_dtd(modes[1]))
    else:
        descs.append(make_dummy_descriptor())
    descs.append(make_range_descriptor(vmin, vmax, hmin, hmax, max_pclk_mhz))
    descs.append(make_name_descriptor(monitor_name))
    edid[54:72] = descs[0]
    edid[72:90] = descs[1]
    edid[90:108] = descs[2]
    edid[108:126] = descs[3]

    edid[126] = 0  # extension count
    # Checksum
    s = sum(edid[:127]) & 0xFF
    edid[127] = (256 - s) & 0xFF
    return bytes(edid)


def make_cea_extension(extra_modes=None):
    """CEA-861 extension block with extra DTDs for high-refresh modes."""
    if extra_modes is None:
        extra_modes = []
    ext = bytearray(128)
    ext[0] = 0x02  # CEA extension tag
    ext[1] = 0x03  # revision 3
    # DTD offset = where DTDs start. 4 means no data block payload (other than 0 bytes).
    # We'll keep no data block payload to maximize DTD space. DTD offset = 4.
    dtd_offset = 4
    ext[2] = dtd_offset
    # Byte 3: support flags (no audio/YCbCr support advertised aggressively)
    ext[3] = 0x00
    # No data blocks payload — go straight to DTDs
    pos = dtd_offset
    # We have 127 - dtd_offset - 1(checksum) = 122 bytes; 6 DTDs max.
    max_dtds = (127 - dtd_offset) // 18
    for m in extra_modes[:max_dtds]:
        dtd = make_dtd(m)
        ext[pos : pos + 18] = dtd
        pos += 18
    # Pad with zeros up to byte 126
    # Checksum
    s = sum(ext[:127]) & 0xFF
    ext[127] = (256 - s) & 0xFF
    return bytes(ext)


def edid_for_mode_set(name, modes, vmin=23, vmax=250, hmin=15, hmax=255, max_pclk_mhz=160):
    """
    Build a complete EDID where modes[0] is preferred. If >2 modes, the rest go
    in a CEA-861 extension as additional DTDs.
    """
    base = make_base_block(modes, monitor_name=name, vmin=vmin, vmax=vmax, hmin=hmin, hmax=hmax, max_pclk_mhz=max_pclk_mhz)
    if len(modes) > 2:
        ext = make_cea_extension(extra_modes=modes[2:])
        # Set extension count
        base = bytearray(base)
        base[126] = 1
        # Recompute base checksum
        s = sum(base[:127]) & 0xFF
        base[127] = (256 - s) & 0xFF
        base = bytes(base)
        return base + ext
    return base


def show_mode(t):
    return (
        f"{t['h_active']}x{t['v_active']}@{t['refresh']:.2f}Hz "
        f"pclk={t['pclk_hz']/1e6:.2f}MHz "
        f"htot={t['h_total']} vtot={t['v_total']} "
        f"hsync_freq={t['pclk_hz']/t['h_total']/1000:.1f}kHz"
    )


def cvt_standard(h_active, v_active, refresh):
    """CVT v1 (non-reduced blanking). Wider blanking — sometimes more chip-tolerant."""
    h_active = (h_active // 8) * 8
    H_SYNC_PER_LINE = 0.08  # CVT spec: 8% of htotal for hsync
    MIN_VSYNC_BP_US = 550
    MIN_V_PORCH = 3
    V_SYNC = _v_sync_for_aspect(h_active, v_active)
    h_period_est = (1_000_000 / refresh - MIN_VSYNC_BP_US) / (v_active + MIN_V_PORCH)
    v_sync_bp_lines = math.ceil(MIN_VSYNC_BP_US / h_period_est)
    v_back = v_sync_bp_lines - V_SYNC
    if v_back < 6:
        v_back = 6
    v_total = v_active + MIN_V_PORCH + V_SYNC + v_back
    # Ideal duty cycle
    ideal_duty = 30 - 300 * (h_period_est / 1000.0)
    if ideal_duty < 20:
        ideal_duty = 20
    h_total = (h_active * 100 / (100 - ideal_duty))
    h_total = int(h_total / 8) * 8
    h_blank = h_total - h_active
    h_sync = int(h_total * 0.08 / 8) * 8
    h_back = h_blank // 2
    h_front = h_blank - h_back - h_sync
    pclk_khz_target = (v_total * h_total * refresh) / 1000
    pclk_khz = int(pclk_khz_target / 250) * 250
    return {
        "h_active": h_active, "h_blank": h_blank, "h_front": h_front, "h_sync": h_sync,
        "v_active": v_active, "v_blank": v_total - v_active,
        "v_front": MIN_V_PORCH, "v_sync": V_SYNC,
        "h_total": h_total, "v_total": v_total,
        "pclk_hz": pclk_khz * 1000,
        "refresh": pclk_khz * 1000 / (h_total * v_total),
        "hsync_pos": False, "vsync_pos": True,  # standard CVT polarity
    }


CANDIDATES = {
    "480p120":   ("JetKVM 480p120",  [cvt_rb(854, 480, 120)]),
    "480p144":   ("JetKVM 480p144",  [cvt_rb(854, 480, 144)]),
    "480p144s":  ("JetKVM 480p144s", [cvt_standard(854, 480, 144)]),
    "480p165":   ("JetKVM 480p165",  [cvt_rb(854, 480, 165)]),
    "480p180":   ("JetKVM 480p180",  [cvt_rb(854, 480, 180)]),
    "480p200":   ("JetKVM 480p200",  [cvt_rb(854, 480, 200)]),
    "480p240":   ("JetKVM 480p240",  [cvt_rb(854, 480, 240)]),
    "480p240s":  ("JetKVM 480p240s", [cvt_standard(854, 480, 240)]),
    "720p100":   ("JetKVM 720p100",  [cvt_rb(1280, 720, 100)]),
    "720p120":   ("JetKVM 720p120",  [cvt_rb(1280, 720, 120)]),
    "720p144":   ("JetKVM 720p144",  [cvt_rb(1280, 720, 144)]),
    # Probe: send a battery of 480p modes at different refresh in one EDID
    "probe480":  ("JetKVM probe480", [
        cvt_rb(854, 480, 120),
        cvt_rb(854, 480, 144),
        cvt_rb(854, 480, 165),
        cvt_rb(854, 480, 180),
        cvt_rb(854, 480, 200),
    ]),
    # Probe: refresh rates near the 120 Hz cliff
    "probe120":  ("JetKVM probe120", [
        cvt_rb(854, 480, 120),
        cvt_rb(854, 480, 125),
        cvt_rb(854, 480, 130),
        cvt_rb(854, 480, 135),
        cvt_rb(854, 480, 140),
    ]),
    # Probe: 720p at variable refresh
    "probe720":  ("JetKVM probe720", [
        cvt_rb(1280, 720, 120),
        cvt_rb(1280, 720, 130),
        cvt_rb(1280, 720, 140),
        cvt_rb(1280, 720, 150),
    ]),
    # Probe: alt resolutions at 120 Hz to characterize chip
    "probealt": ("JetKVM probealt", [
        cvt_rb(1024, 768, 120),
        cvt_rb(960, 540, 120),
        cvt_rb(960, 540, 144),
        cvt_rb(640, 480, 144),
        cvt_rb(800, 600, 144),
    ]),
    # Combined: only modes the TC358743 chip actually locks onto
    # (chip has a hard ~120 Hz vrefresh ceiling)
    "combo":     ("JetKVM 120Hz", [
        cvt_rb(854, 480, 120),
        cvt_rb(1280, 720, 120),
    ]),
}


def main():
    if len(sys.argv) < 2:
        print("Usage: edid_gen.py <mode>")
        print(f"Modes: {', '.join(CANDIDATES.keys())}")
        sys.exit(1)
    key = sys.argv[1]
    if key not in CANDIDATES:
        print(f"unknown mode {key}")
        sys.exit(1)
    name, modes = CANDIDATES[key]
    for m in modes:
        print(f"# {show_mode(m)}", file=sys.stderr)
    edid = edid_for_mode_set(name, modes)
    print(edid.hex())


if __name__ == "__main__":
    main()
