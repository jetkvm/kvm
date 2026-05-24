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

// PCMUToLinear decodes a G.711 mu-law sample to signed 16-bit PCM.
func PCMUToLinear(sample byte) int16 {
	u := ^sample
	pcm := ((int(u) & 0x0f) << 3) + pcmuBias
	pcm <<= (uint(u) & 0x70) >> 4
	if u&0x80 != 0 {
		return int16(pcmuBias - pcm)
	}
	return int16(pcm - pcmuBias)
}

// ApplyPCM16Gain multiplies a PCM sample and saturates at int16 bounds.
func ApplyPCM16Gain(sample int16, gain int) int16 {
	if gain <= 1 {
		return sample
	}
	scaled := int(sample) * gain
	if scaled > 32767 {
		return 32767
	}
	if scaled < -32768 {
		return -32768
	}
	return int16(scaled)
}
