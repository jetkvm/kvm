package audio

var (
	g722Q6 = [32]int{
		0, 35, 72, 110, 150, 190, 233, 276,
		323, 370, 422, 473, 530, 587, 650, 714,
		786, 858, 940, 1023, 1121, 1219, 1339, 1458,
		1612, 1765, 1980, 2195, 2557, 2919, 0, 0,
	}
	g722ILN = [32]int{
		0, 63, 62, 31, 30, 29, 28, 27,
		26, 25, 24, 23, 22, 21, 20, 19,
		18, 17, 16, 15, 14, 13, 12, 11,
		10, 9, 8, 7, 6, 5, 4, 0,
	}
	g722ILP = [32]int{
		0, 61, 60, 59, 58, 57, 56, 55,
		54, 53, 52, 51, 50, 49, 48, 47,
		46, 45, 44, 43, 42, 41, 40, 39,
		38, 37, 36, 35, 34, 33, 32, 0,
	}
	g722WL   = [8]int{-60, -30, 58, 172, 334, 538, 1198, 3042}
	g722RL42 = [16]int{
		0, 7, 6, 5, 4, 3, 2, 1,
		7, 6, 5, 4, 3, 2, 1, 0,
	}
	g722ILB = [32]int{
		2048, 2093, 2139, 2186, 2233, 2282, 2332, 2383,
		2435, 2489, 2543, 2599, 2656, 2714, 2774, 2834,
		2896, 2960, 3025, 3091, 3158, 3228, 3298, 3371,
		3444, 3520, 3597, 3676, 3756, 3838, 3922, 4008,
	}
	g722QM4 = [16]int{
		0, -20456, -12896, -8968,
		-6288, -4240, -2584, -1200,
		20456, 12896, 8968, 6288,
		4240, 2584, 1200, 0,
	}
	g722QM2 = [4]int{-7408, -1616, 7408, 1616}
	g722QMF = [12]int{3, -11, 12, 32, -210, 951, 3876, -805, 362, -156, 53, -11}
	g722IHN = [3]int{0, 1, 0}
	g722IHP = [3]int{0, 3, 2}
	g722WH  = [3]int{0, -214, 798}
	g722RH2 = [4]int{2, 1, 2, 1}
)

type G722Encoder struct {
	x    [24]int
	band [2]g722Band
}

type g722Band struct {
	s   int
	sp  int
	sz  int
	r   [3]int
	a   [3]int
	ap  [3]int
	p   [3]int
	d   [7]int
	b   [7]int
	bp  [7]int
	sg  [7]int
	nb  int
	det int
}

func NewG722Encoder() *G722Encoder {
	e := &G722Encoder{}
	e.band[0].det = 32
	e.band[1].det = 8
	return e
}

func (e *G722Encoder) Encode(dst []byte, samples []int16) int {
	out := 0
	for j := 0; j+1 < len(samples) && out < len(dst); {
		copy(e.x[0:22], e.x[2:24])
		e.x[22] = int(samples[j])
		j++
		e.x[23] = int(samples[j])
		j++

		sumOdd := 0
		sumEven := 0
		for i := 0; i < 12; i++ {
			sumOdd += e.x[2*i] * g722QMF[i]
			sumEven += e.x[2*i+1] * g722QMF[11-i]
		}

		xlow := (sumEven + sumOdd) >> 14
		xhigh := (sumEven - sumOdd) >> 14

		el := g722Saturate(xlow - e.band[0].s)
		wd := el
		if wd < 0 {
			wd = -(wd + 1)
		}

		i := 1
		for ; i < 30; i++ {
			if wd < (g722Q6[i]*e.band[0].det)>>12 {
				break
			}
		}

		ilow := g722ILP[i]
		if el < 0 {
			ilow = g722ILN[i]
		}

		ril := ilow >> 2
		dlow := (e.band[0].det * g722QM4[ril]) >> 15
		il4 := g722RL42[ril]
		wd = (e.band[0].nb * 127) >> 7
		e.band[0].nb = wd + g722WL[il4]
		if e.band[0].nb < 0 {
			e.band[0].nb = 0
		} else if e.band[0].nb > 18432 {
			e.band[0].nb = 18432
		}

		wd1 := (e.band[0].nb >> 6) & 31
		wd2 := 8 - (e.band[0].nb >> 11)
		wd3 := g722ILB[wd1]
		if wd2 < 0 {
			wd3 <<= -wd2
		} else {
			wd3 >>= wd2
		}
		e.band[0].det = wd3 << 2
		e.block4(0, dlow)

		eh := g722Saturate(xhigh - e.band[1].s)
		wd = eh
		if wd < 0 {
			wd = -(wd + 1)
		}

		mih := 1
		if wd >= (564*e.band[1].det)>>12 {
			mih = 2
		}

		ihigh := g722IHP[mih]
		if eh < 0 {
			ihigh = g722IHN[mih]
		}

		dhigh := (e.band[1].det * g722QM2[ihigh]) >> 15
		ih2 := g722RH2[ihigh]
		wd = (e.band[1].nb * 127) >> 7
		e.band[1].nb = wd + g722WH[ih2]
		if e.band[1].nb < 0 {
			e.band[1].nb = 0
		} else if e.band[1].nb > 22528 {
			e.band[1].nb = 22528
		}

		wd1 = (e.band[1].nb >> 6) & 31
		wd2 = 10 - (e.band[1].nb >> 11)
		wd3 = g722ILB[wd1]
		if wd2 < 0 {
			wd3 <<= -wd2
		} else {
			wd3 >>= wd2
		}
		e.band[1].det = wd3 << 2
		e.block4(1, dhigh)

		dst[out] = byte((ihigh << 6) | ilow)
		out++
	}
	return out
}

func (e *G722Encoder) block4(bandIndex int, d int) {
	b := &e.band[bandIndex]

	b.d[0] = d
	b.r[0] = g722Saturate(b.s + d)
	b.p[0] = g722Saturate(b.sz + d)

	for i := 0; i < 3; i++ {
		b.sg[i] = b.p[i] >> 15
	}
	wd1 := g722Saturate(b.a[1] << 2)
	wd2 := wd1
	if b.sg[0] == b.sg[1] {
		wd2 = -wd1
	}
	if wd2 > 32767 {
		wd2 = 32767
	}
	wd3 := wd2 >> 7
	if b.sg[0] == b.sg[2] {
		wd3 += 128
	} else {
		wd3 -= 128
	}
	wd3 += (b.a[2] * 32512) >> 15
	if wd3 > 12288 {
		wd3 = 12288
	} else if wd3 < -12288 {
		wd3 = -12288
	}
	b.ap[2] = wd3

	b.sg[0] = b.p[0] >> 15
	b.sg[1] = b.p[1] >> 15
	wd1 = -192
	if b.sg[0] == b.sg[1] {
		wd1 = 192
	}
	wd2 = (b.a[1] * 32640) >> 15
	b.ap[1] = g722Saturate(wd1 + wd2)
	wd3 = g722Saturate(15360 - b.ap[2])
	if b.ap[1] > wd3 {
		b.ap[1] = wd3
	} else if b.ap[1] < -wd3 {
		b.ap[1] = -wd3
	}

	wd1 = 0
	if d != 0 {
		wd1 = 128
	}
	b.sg[0] = d >> 15
	for i := 1; i < 7; i++ {
		b.sg[i] = b.d[i] >> 15
		wd2 = wd1
		if b.sg[i] != b.sg[0] {
			wd2 = -wd1
		}
		wd3 = (b.b[i] * 32640) >> 15
		b.bp[i] = g722Saturate(wd2 + wd3)
	}

	for i := 6; i > 0; i-- {
		b.d[i] = b.d[i-1]
		b.b[i] = b.bp[i]
	}
	for i := 2; i > 0; i-- {
		b.r[i] = b.r[i-1]
		b.p[i] = b.p[i-1]
		b.a[i] = b.ap[i]
	}

	wd1 = g722Saturate(b.r[1] + b.r[1])
	wd1 = (b.a[1] * wd1) >> 15
	wd2 = g722Saturate(b.r[2] + b.r[2])
	wd2 = (b.a[2] * wd2) >> 15
	b.sp = g722Saturate(wd1 + wd2)

	b.sz = 0
	for i := 6; i > 0; i-- {
		wd1 = g722Saturate(b.d[i] + b.d[i])
		b.sz += (b.b[i] * wd1) >> 15
	}
	b.sz = g722Saturate(b.sz)
	b.s = g722Saturate(b.sp + b.sz)
}

func g722Saturate(v int) int {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}
