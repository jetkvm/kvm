package rfb

// SplitAnnexB parses an H.264 Annex-B byte stream into its constituent
// NAL units. Both 4-byte (00 00 00 01) and 3-byte (00 00 01) start
// codes are recognised. Returned slices reference the input buffer
// (no copy).
//
// If no start codes are found, the entire frame is treated as a
// single NAL unit. Empty input yields an empty result.
//
// Used by the OpenH264 (encoding 50) path to extract SPS/PPS NALs for
// late-joiner priming and to detect IDR frames.
func SplitAnnexB(frame []byte) [][]byte {
	var nalus [][]byte
	start := -1
	n := len(frame)

	for i := 0; i <= n-3; i++ {
		if frame[i] == 0 && frame[i+1] == 0 {
			// 4-byte start code 00 00 00 01
			if i <= n-4 && frame[i+2] == 0 && frame[i+3] == 1 {
				if start >= 0 {
					nalus = append(nalus, frame[start:i])
				}
				start = i + 4
				i += 3
				continue
			}
			// 3-byte start code 00 00 01
			if frame[i+2] == 1 {
				if start >= 0 {
					nalus = append(nalus, frame[start:i])
				}
				start = i + 3
				i += 2
				continue
			}
		}
	}

	// Trailing NALU
	if start >= 0 && start < n {
		nalus = append(nalus, frame[start:n])
	}

	// If no start codes were found, treat the entire frame as a
	// single raw NALU.
	if len(nalus) == 0 && n > 0 {
		return [][]byte{frame}
	}
	return nalus
}
