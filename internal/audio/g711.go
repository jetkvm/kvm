package audio

const (
	pcmuBias = 0x84
	pcmuClip = 32635
)

// LinearToPCMU encodes a signed 16-bit PCM sample as G.711 mu-law.
func LinearToPCMU(sample int16) byte {
	pcm := int(sample)
	mask := 0xff
	if pcm < 0 {
		pcm = -pcm
		mask = 0x7f
	}
	if pcm > pcmuClip {
		pcm = pcmuClip
	}

	pcm += pcmuBias
	segment := 7
	for expMask := 0x4000; segment > 0 && pcm&expMask == 0; segment-- {
		expMask >>= 1
	}

	quantized := 0
	if segment == 0 {
		quantized = (pcm >> 4) & 0x0f
	} else {
		quantized = (pcm >> (segment + 3)) & 0x0f
	}

	return byte(^(segment<<4 | quantized) & mask)
}
