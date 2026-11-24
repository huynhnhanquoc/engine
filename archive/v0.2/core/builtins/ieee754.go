package builtins

import "math"

// The functions of Math as V8 computes them, so that a program's Math.sin(2) is node's to the last bit:
// V8 runs fdlibm (Sun's freely distributable libm, 1993) and a few of FreeBSD's msun descendants of it
// (src/base/ieee754.cc), where Go's math package uses other algorithms or assembly that differ in the
// last bit on about a quarter of the arguments (and pow on half). Each function below is that C code in
// Go, with the constants by their bit patterns and fdlibm's short names for its variables (hx the high
// word of x, ix that of |x|, lx the low word, hfsq half f squared…), kept so each line can be read
// against the C it came from, which is the only way to check a port like this.
//
// Every product that is added to or taken from something is written float64(a * b). The Go
// specification lets a compiler fuse a*b + c into one rounding (gc does on arm64, ppc64, s390x, riscv64
// and on amd64 from GOAMD64=v3), which changes the last bit; an explicit conversion rounds the product on
// its own and so forbids the fusion, as fdlibm's C, compiled without contraction, rounds it.

// ---- the words of a float64, as fdlibm reads and writes them ----

func highWord(x float64) int32 { return int32(math.Float64bits(x) >> 32) }
func lowWord(x float64) uint32 { return uint32(math.Float64bits(x)) }

func fromWords(high int32, low uint32) float64 {
	return math.Float64frombits(uint64(uint32(high))<<32 | uint64(low))
}

func withHighWord(x float64, high int32) float64 { return fromWords(high, lowWord(x)) }
func withLowWord(x float64, low uint32) float64  { return fromWords(highWord(x), low) }

// ---- argument reduction for sin, cos and tan ----

// remainderPiOver2 is __ieee754_rem_pio2: x - n*pi/2 as y0 + y1, |y0 + y1| <= pi/4, and n.
func remainderPiOver2(x float64) (n int32, y0, y1 float64) {
	const (
		invpio2 = 6.36619772367581382433e-01 // 0x3FE45F30, 0x6DC9C883
		pio2_1  = 1.57079632673412561417e+00 // 0x3FF921FB, 0x54400000
		pio2_1t = 6.07710050650619224932e-11 // 0x3DD0B461, 0x1A626331
		pio2_2  = 6.07710050630396597660e-11 // 0x3DD0B461, 0x1A600000
		pio2_2t = 2.02226624879595063154e-21 // 0x3BA3198A, 0x2E037073
		pio2_3  = 2.02226624871116645580e-21 // 0x3BA3198A, 0x2E000000
		pio2_3t = 8.47842766036889956997e-32 // 0x397B839A, 0x252049C1
		two24   = 1.67772160000000000000e+07
	)
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	if ix <= 0x3FE921FB { // |x| ~<= pi/4: nothing to reduce
		return 0, x, 0
	}
	if ix < 0x4002D97C { // |x| < 3pi/4: n is 1 or -1
		if hx > 0 {
			z := x - pio2_1
			if ix != 0x3FF921FB { // 33+53 bits of pi are enough
				y0 = z - pio2_1t
				y1 = (z - y0) - pio2_1t
			} else { // near pi/2: 33+33+53 bits
				z -= pio2_2
				y0 = z - pio2_2t
				y1 = (z - y0) - pio2_2t
			}
			return 1, y0, y1
		}
		z := x + pio2_1
		if ix != 0x3FF921FB {
			y0 = z + pio2_1t
			y1 = (z - y0) + pio2_1t
		} else {
			z += pio2_2
			y0 = z + pio2_2t
			y1 = (z - y0) + pio2_2t
		}
		return -1, y0, y1
	}
	if ix <= 0x413921FB { // |x| ~<= 2^19 * (pi/2): medium size
		t := math.Abs(x)
		n = int32(float64(t*invpio2) + 0.5)
		fn := float64(n)
		r := t - float64(fn*pio2_1)
		w := float64(fn * pio2_1t) // first round, good to 85 bits
		if n < 32 && ix != piOver2Multiples[n-1] {
			y0 = r - w // quick check: no cancellation
		} else {
			j := ix >> 20
			y0 = r - w
			i := j - (highWord(y0)>>20)&0x7FF
			if i > 16 { // second iteration, good to 118 bits
				t = r
				w = float64(fn * pio2_2)
				r = t - w
				w = float64(fn*pio2_2t) - ((t - r) - w)
				y0 = r - w
				i = j - (highWord(y0)>>20)&0x7FF
				if i > 49 { // third iteration, 151 bits, covers every case
					t = r
					w = float64(fn * pio2_3)
					r = t - w
					w = float64(fn*pio2_3t) - ((t - r) - w)
					y0 = r - w
				}
			}
		}
		y1 = (r - y0) - w
		if hx < 0 {
			return -n, -y0, -y1
		}
		return n, y0, y1
	}
	if ix >= 0x7FF00000 { // infinity or NaN
		return 0, x - x, x - x
	}
	// a large argument: z = scalbn(|x|, ilogb(x) - 23), cut into 24-bit pieces
	e0 := (ix >> 20) - 1046
	z := fromWords(ix-int32(uint32(e0)<<20), lowWord(x))
	var tx [3]float64
	for i := 0; i < 2; i++ {
		tx[i] = float64(int32(z))
		z = float64((z - tx[i]) * two24)
	}
	tx[2] = z
	nx := 3
	for tx[nx-1] == 0 {
		nx--
	}
	n, y0, y1 = kernelRemainderPiOver2(tx[:nx], int(e0))
	if hx < 0 {
		return -n, -y0, -y1
	}
	return n, y0, y1
}

// piOver2Multiples is the high word of n*pi/2 for n = 1 to 32 (npio2_hw).
var piOver2Multiples = [32]int32{
	0x3FF921FB, 0x400921FB, 0x4012D97C, 0x401921FB, 0x401F6A7A, 0x4022D97C,
	0x4025FDBB, 0x402921FB, 0x402C463A, 0x402F6A7A, 0x4031475C, 0x4032D97C,
	0x40346B9C, 0x4035FDBB, 0x40378FDB, 0x403921FB, 0x403AB41B, 0x403C463A,
	0x403DD85A, 0x403F6A7A, 0x40407E4C, 0x4041475C, 0x4042106C, 0x4042D97C,
	0x4043A28C, 0x40446B9C, 0x404534AC, 0x4045FDBB, 0x4046C6CB, 0x40478FDB,
	0x404858EB, 0x404921FB,
}

// twoOverPi is 2/pi in 24-bit pieces, 396 hexadecimal digits (two_over_pi).
var twoOverPi = [66]int32{
	0xA2F983, 0x6E4E44, 0x1529FC, 0x2757D1, 0xF534DD, 0xC0DB62, 0x95993C,
	0x439041, 0xFE5163, 0xABDEBB, 0xC561B7, 0x246E3A, 0x424DD2, 0xE00649,
	0x2EEA09, 0xD1921C, 0xFE1DEB, 0x1CB129, 0xA73EE8, 0x8235F5, 0x2EBB44,
	0x84E99C, 0x7026B4, 0x5F7E41, 0x3991D6, 0x398353, 0x39F49C, 0x845F8B,
	0xBDF928, 0x3B1FF8, 0x97FFDE, 0x05980F, 0xEF2F11, 0x8B5A0A, 0x6D1F6D,
	0x367ECF, 0x27CB09, 0xB74F46, 0x3F669E, 0x5FEA2D, 0x7527BA, 0xC7EBE5,
	0xF17B3D, 0x0739F7, 0x8A5292, 0xEA6BFB, 0x5FB11F, 0x8D5D08, 0x560330,
	0x46FC7B, 0x6BABF0, 0xCFBC20, 0x9AF436, 0x1DA9E3, 0x91615E, 0xE61B08,
	0x659985, 0x5F14A0, 0x68408D, 0xFFD880, 0x4D7327, 0x310606, 0x1556CA,
	0x73A8C9, 0x60E27B, 0xC08C6B,
}

// piOver2Pieces is pi/2 in pieces of 24 bits (PIo2).
var piOver2Pieces = [8]float64{
	1.57079625129699707031e+00, // 0x3FF921FB, 0x40000000
	7.54978941586159635335e-08, // 0x3E74442D, 0x00000000
	5.39030252995776476554e-15, // 0x3CF84698, 0x80000000
	3.28200341580791294123e-22, // 0x3B78CC51, 0x60000000
	1.27065575308067607349e-29, // 0x39F01B83, 0x80000000
	1.22933308981111328932e-36, // 0x387A2520, 0x40000000
	2.73370053816464559624e-44, // 0x36E38222, 0x80000000
	2.16741683877804819444e-51, // 0x3569F31D, 0x00000000
}

// kernelRemainderPiOver2 is __kernel_rem_pio2 at double precision (prec 2): x, the argument in 24-bit
// pieces scaled by 2^-e0, reduced by pi/2 with as many bits of 2/pi as it needs (Payne and Hanek).
func kernelRemainderPiOver2(x []float64, e0 int) (int32, float64, float64) {
	const (
		two24  = 1.67772160000000000000e+07 // 0x41700000, 0x00000000
		twon24 = 5.96046447753906250000e-08 // 0x3E700000, 0x00000000
		jk     = 4                          // init_jk[2]: terms of 2/pi beyond the argument's
	)
	var (
		iq           [20]int32
		f, fq, q     [20]float64
		jx           = len(x) - 1
		jv           = max((e0-3)/24, 0)
		q0           = e0 - 24*(jv+1)
		jp           = jk
		z, fw        float64
		n, ih, carry int32
	)
	// f[0] to f[jx+jk] are ipio2[jv-jx] to ipio2[jv+jk]
	for i, j := 0, jv-jx; i <= jx+jk; i, j = i+1, j+1 {
		if j >= 0 {
			f[i] = float64(twoOverPi[j])
		}
	}
	for i := 0; i <= jk; i++ {
		fw = 0
		for j := 0; j <= jx; j++ {
			fw += float64(x[j] * f[jx+i-j])
		}
		q[i] = fw
	}
	jz := jk
	for {
		// distill q[] into iq[], from the top
		i, j := 0, jz
		for z = q[jz]; j > 0; i, j = i+1, j-1 {
			fw = float64(int32(float64(twon24 * z)))
			iq[i] = int32(z - float64(two24*fw))
			z = q[j-1] + fw
		}
		z = math.Ldexp(z, q0)
		z -= float64(8.0 * math.Floor(float64(z*0.125))) // the integer part below 8
		n = int32(z)
		z -= float64(n)
		ih = 0
		switch {
		case q0 > 0: // iq[jz-1] tells n
			i := iq[jz-1] >> (24 - q0)
			n += i
			iq[jz-1] -= i << (24 - q0)
			ih = iq[jz-1] >> (23 - q0)
		case q0 == 0:
			ih = iq[jz-1] >> 23
		case z >= 0.5:
			ih = 2
		}
		if ih > 0 { // q > 0.5
			n++
			carry = 0
			for i := 0; i < jz; i++ { // 1 - q
				j := iq[i]
				if carry == 0 {
					if j != 0 {
						carry = 1
						iq[i] = 0x1000000 - j
					}
				} else {
					iq[i] = 0xFFFFFF - j
				}
			}
			switch q0 { // rare: one chance in 12
			case 1:
				iq[jz-1] &= 0x7FFFFF
			case 2:
				iq[jz-1] &= 0x3FFFFF
			}
			if ih == 2 {
				z = 1 - z
				if carry != 0 {
					z -= math.Ldexp(1, q0)
				}
			}
		}
		// recompute with more terms of 2/pi when every bit so far cancelled
		if z == 0 {
			j := int32(0)
			for i := jz - 1; i >= jk; i-- {
				j |= iq[i]
			}
			if j == 0 {
				k := 1
				for ; jk >= k && iq[jk-k] == 0; k++ {
				}
				for i := jz + 1; i <= jz+k; i++ {
					f[jx+i] = float64(twoOverPi[jv+i])
					fw = 0
					for j := 0; j <= jx; j++ {
						fw += float64(x[j] * f[jx+i-j])
					}
					q[i] = fw
				}
				jz += k
				continue
			}
		}
		break
	}
	// drop the zero terms, or cut z into 24-bit pieces
	if z == 0 {
		jz--
		q0 -= 24
		for iq[jz] == 0 {
			jz--
			q0 -= 24
		}
	} else {
		z = math.Ldexp(z, -q0)
		if z >= two24 {
			fw = float64(int32(float64(twon24 * z)))
			iq[jz] = int32(z - float64(two24*fw))
			jz++
			q0 += 24
			iq[jz] = int32(fw)
		} else {
			iq[jz] = int32(z)
		}
	}
	fw = math.Ldexp(1, q0)
	for i := jz; i >= 0; i-- {
		q[i] = float64(fw * float64(iq[i]))
		fw *= twon24
	}
	for i := jz; i >= 0; i-- {
		fw = 0
		for k := 0; k <= jp && k <= jz-i; k++ {
			fw += float64(piOver2Pieces[k] * q[i+k])
		}
		fq[jz-i] = fw
	}
	fw = 0
	for i := jz; i >= 0; i-- {
		fw += fq[i]
	}
	y0 := fw
	if ih != 0 {
		y0 = -fw
	}
	fw = fq[0] - fw
	for i := 1; i <= jz; i++ {
		fw += fq[i]
	}
	y1 := fw
	if ih != 0 {
		y1 = -fw
	}
	return n & 7, y0, y1
}

// ---- sin, cos, tan ----

// kernelSin is __kernel_sin: sin(x + y) for |x| ~<= pi/4, y the tail of x; tail says whether y counts.
func kernelSin(x, y float64, tail bool) float64 {
	const (
		S1 = -1.66666666666666324348e-01 // 0xBFC55555, 0x55555549
		S2 = 8.33333333332248946124e-03  // 0x3F811111, 0x1110F8A6
		S3 = -1.98412698298579493134e-04 // 0xBF2A01A0, 0x19C161D5
		S4 = 2.75573137070700676789e-06  // 0x3EC71DE3, 0x57B1FE7D
		S5 = -2.50507602534068634195e-08 // 0xBE5AE5E6, 0x8A2B9CEB
		S6 = 1.58969099521155010221e-10  // 0x3DE5D93A, 0x5ACFD57C
	)
	if highWord(x)&0x7FFFFFFF < 0x3E400000 && int32(x) == 0 { // |x| < 2^-27
		return x
	}
	z := float64(x * x)
	v := float64(z * x)
	r := S2 + float64(z*(S3+float64(z*(S4+float64(z*(S5+float64(z*S6)))))))
	if !tail {
		return x + float64(v*(S1+float64(z*r)))
	}
	return x - ((float64(z*(float64(0.5*y)-float64(v*r))) - y) - float64(v*S1))
}

// kernelCos is __kernel_cos: cos(x + y) for |x| ~<= pi/4.
func kernelCos(x, y float64) float64 {
	const (
		C1 = 4.16666666666666019037e-02  // 0x3FA55555, 0x5555554C
		C2 = -1.38888888888741095749e-03 // 0xBF56C16C, 0x16C15177
		C3 = 2.48015872894767294178e-05  // 0x3EFA01A0, 0x19CB1590
		C4 = -2.75573143513906633035e-07 // 0xBE927E4F, 0x809C52AD
		C5 = 2.08757232129817482790e-09  // 0x3E21EE9E, 0xBDB4B1C4
		C6 = -1.13596475577881948265e-11 // 0xBDA8FAE9, 0xBE8838D4
	)
	ix := highWord(x) & 0x7FFFFFFF
	if ix < 0x3E400000 && int32(x) == 0 { // |x| < 2^-27
		return 1
	}
	z := float64(x * x)
	r := float64(z * (C1 + float64(z*(C2+float64(z*(C3+float64(z*(C4+float64(z*(C5+float64(z*C6)))))))))))
	if ix < 0x3FD33333 { // |x| < 0.3
		return 1 - (float64(0.5*z) - (float64(z*r) - float64(x*y)))
	}
	qx := 0.28125
	if ix <= 0x3FE90000 { // |x| <= 0.78125: x/4
		qx = fromWords(ix-0x00200000, 0)
	}
	hz := float64(0.5*z) - qx
	a := 1 - qx
	return a - (hz - (float64(z*r) - float64(x*y)))
}

// tanCoefficients are __kernel_tan's T[0] to T[12].
var tanCoefficients = [13]float64{
	3.33333333333334091986e-01,  // 3FD55555, 55555563
	1.33333333333201242699e-01,  // 3FC11111, 1110FE7A
	5.39682539762260521377e-02,  // 3FABA1BA, 1BB341FE
	2.18694882948595424599e-02,  // 3F9664F4, 8406D637
	8.86323982359930005737e-03,  // 3F8226E3, E96E8493
	3.59207910759131235356e-03,  // 3F6D6D22, C9560328
	1.45620945432529025516e-03,  // 3F57DBC8, FEE08315
	5.88041240820264096874e-04,  // 3F4344D8, F2F26501
	2.46463134818469906812e-04,  // 3F3026F7, 1A8D1068
	7.81794442939557092300e-05,  // 3F147E88, A03792A6
	7.14072491382608190305e-05,  // 3F12B80F, 32F0A7E9
	-1.85586374855275456654e-05, // BEF375CB, DB605373
	2.59073051863633712884e-05,  // 3EFB2A70, 74BF7AD4
}

// kernelTan is __kernel_tan: tan(x + y) for |x| ~<= pi/4 when odd is 1, -1/tan(x + y) when it is -1.
func kernelTan(x, y float64, odd int32) float64 {
	const (
		pio4   = 7.85398163397448278999e-01 // 3FE921FB, 54442D18
		pio4lo = 3.06161699786838301793e-17 // 3C81A626, 33145C07
	)
	T := &tanCoefficients
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	if ix < 0x3E300000 && int32(x) == 0 { // |x| < 2^-28
		switch {
		case (uint32(ix)|lowWord(x))|uint32(odd+1) == 0:
			return 1 / math.Abs(x)
		case odd == 1:
			return x
		}
		// -1/(x + y), carefully
		w := x + y
		z := withLowWord(w, 0)
		v := y - (z - x)
		a := -1 / w
		t := withLowWord(a, 0)
		s := 1 + float64(t*z)
		return t + float64(a*(s+float64(t*v)))
	}
	if ix >= 0x3FE59428 { // |x| >= 0.6744
		if hx < 0 {
			x, y = -x, -y
		}
		z := pio4 - x
		w := pio4lo - y
		x = z + w
		y = 0
	}
	z := float64(x * x)
	w := float64(z * z)
	// x^5 (T[1] + x^2 T[2] + …) split into x^5 (T[1] + x^4 T[3] + … + x^20 T[11]) and
	// x^5 (x^2 (T[2] + x^4 T[4] + … + x^22 T[12]))
	r := T[1] + float64(w*(T[3]+float64(w*(T[5]+float64(w*(T[7]+float64(w*(T[9]+float64(w*T[11])))))))))
	v := float64(z * (T[2] + float64(w*(T[4]+float64(w*(T[6]+float64(w*(T[8]+float64(w*(T[10]+float64(w*T[12])))))))))))
	s := float64(z * x)
	r = y + float64(z*(float64(s*(r+v))+y))
	r += float64(T[0] * s)
	w = x + r
	if ix >= 0x3FE59428 {
		v = float64(odd)
		return float64(float64(1-((hx>>30)&2)) * (v - float64(2.0*(x-(float64(w*w)/(w+v)-r)))))
	}
	if odd == 1 {
		return w
	}
	// -1/(x + r), accurately
	z = withLowWord(w, 0)
	v = r - (z - x) // z + v = r + x
	a := -1.0 / w
	t := withLowWord(a, 0)
	s = 1.0 + float64(t*z)
	return t + float64(a*(s+float64(t*v)))
}

func fdlibmSin(x float64) float64 {
	ix := highWord(x) & 0x7FFFFFFF
	switch {
	case ix <= 0x3FE921FB:
		return kernelSin(x, 0, false)
	case ix >= 0x7FF00000:
		return x - x
	}
	n, y0, y1 := remainderPiOver2(x)
	switch n & 3 {
	case 0:
		return kernelSin(y0, y1, true)
	case 1:
		return kernelCos(y0, y1)
	case 2:
		return -kernelSin(y0, y1, true)
	}
	return -kernelCos(y0, y1)
}

func fdlibmCos(x float64) float64 {
	ix := highWord(x) & 0x7FFFFFFF
	switch {
	case ix <= 0x3FE921FB:
		return kernelCos(x, 0)
	case ix >= 0x7FF00000:
		return x - x
	}
	n, y0, y1 := remainderPiOver2(x)
	switch n & 3 {
	case 0:
		return kernelCos(y0, y1)
	case 1:
		return -kernelSin(y0, y1, true)
	case 2:
		return -kernelCos(y0, y1)
	}
	return kernelSin(y0, y1, true)
}

func tan(x float64) float64 {
	ix := highWord(x) & 0x7FFFFFFF
	switch {
	case ix <= 0x3FE921FB:
		return kernelTan(x, 0, 1)
	case ix >= 0x7FF00000:
		return x - x
	}
	n, y0, y1 := remainderPiOver2(x)
	return kernelTan(y0, y1, 1-((n&1)<<1)) // 1 for n even, -1 for n odd
}

// ---- asin, acos, atan, atan2 ----

const (
	pio2Hi = 1.57079632679489655800e+00 // 0x3FF921FB, 0x54442D18
	pio2Lo = 6.12323399573676603587e-17 // 0x3C91A626, 0x33145C07
	// the rational approximation of asin and acos, R(x^2) = p/q
	pS0 = 1.66666666666666657415e-01  // 0x3FC55555, 0x55555555
	pS1 = -3.25565818622400915405e-01 // 0xBFD4D612, 0x03EB6F7D
	pS2 = 2.01212532134862925881e-01  // 0x3FC9C155, 0x0E884455
	pS3 = -4.00555345006794114027e-02 // 0xBFA48228, 0xB5688F3B
	pS4 = 7.91534994289814532176e-04  // 0x3F49EFE0, 0x7501B288
	pS5 = 3.47933107596021167570e-05  // 0x3F023DE1, 0x0DFDF709
	qS1 = -2.40339491173441421878e+00 // 0xC0033A27, 0x1C8A2D4B
	qS2 = 2.02094576023350569471e+00  // 0x40002AE5, 0x9C598AC8
	qS3 = -6.88283971605453293030e-01 // 0xBFE6066C, 0x1B8D0159
	qS4 = 7.70381505559019352791e-02  // 0x3FB3B8C5, 0xB12E9282
)

func asinPolynomial(t float64) (p, q float64) {
	p = float64(t * (pS0 + float64(t*(pS1+float64(t*(pS2+float64(t*(pS3+float64(t*(pS4+float64(t*pS5)))))))))))
	q = 1 + float64(t*(qS1+float64(t*(qS2+float64(t*(qS3+float64(t*qS4)))))))
	return p, q
}

func asin(x float64) float64 {
	const pio4Hi = 7.85398163397448278999e-01 // 0x3FE921FB, 0x54442D18
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	switch {
	case ix >= 0x3FF00000: // |x| >= 1
		if uint32(ix-0x3FF00000)|lowWord(x) == 0 { // asin(±1) = ±pi/2
			return float64(x*pio2Hi) + float64(x*pio2Lo)
		}
		return (x - x) / (x - x) // NaN past 1
	case ix < 0x3FE00000: // |x| < 0.5
		if ix < 0x3E400000 { // |x| < 2^-27
			return x
		}
		p, q := asinPolynomial(float64(x * x))
		return x + float64(x*(p/q))
	}
	w := 1 - math.Abs(x) // 0.5 <= |x| < 1
	t := float64(w * 0.5)
	p, q := asinPolynomial(t)
	s := math.Sqrt(t)
	if ix >= 0x3FEF3333 { // |x| > 0.975
		w = p / q
		t = pio2Hi - (float64(2.0*(s+float64(s*w))) - pio2Lo)
	} else {
		w = withLowWord(s, 0)
		c := (t - float64(w*w)) / (s + w)
		r := p / q
		p = float64(float64(2.0*s)*r) - (pio2Lo - float64(2.0*c))
		q = pio4Hi - float64(2.0*w)
		t = pio4Hi - (p - q)
	}
	if hx > 0 {
		return t
	}
	return -t
}

func acos(x float64) float64 {
	const pi = 3.14159265358979311600e+00 // 0x400921FB, 0x54442D18
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	switch {
	case ix >= 0x3FF00000: // |x| >= 1
		if uint32(ix-0x3FF00000)|lowWord(x) == 0 {
			if hx > 0 {
				return 0 // acos(1)
			}
			return pi + float64(2.0*pio2Lo) // acos(-1)
		}
		return (x - x) / (x - x)
	case ix < 0x3FE00000: // |x| < 0.5
		if ix <= 0x3C600000 { // |x| < 2^-57
			return pio2Hi + pio2Lo
		}
		p, q := asinPolynomial(float64(x * x))
		return pio2Hi - (x - (pio2Lo - float64(x*(p/q))))
	case hx < 0: // x < -0.5
		z := float64((1 + x) * 0.5)
		p, q := asinPolynomial(z)
		s := math.Sqrt(z)
		w := float64((p/q)*s) - pio2Lo
		return pi - float64(2.0*(s+w))
	}
	z := float64((1 - x) * 0.5) // x > 0.5
	s := math.Sqrt(z)
	df := withLowWord(s, 0)
	c := (z - float64(df*df)) / (s + df)
	p, q := asinPolynomial(z)
	w := float64((p/q)*s) + c
	return float64(2.0 * (df + w))
}

var (
	atanHi = [4]float64{
		4.63647609000806093515e-01, // atan(0.5) 0x3FDDAC67, 0x0561BB4F
		7.85398163397448278999e-01, // atan(1.0) 0x3FE921FB, 0x54442D18
		9.82793723247329054082e-01, // atan(1.5) 0x3FEF730B, 0xD281F69B
		1.57079632679489655800e+00, // atan(inf) 0x3FF921FB, 0x54442D18
	}
	atanLo = [4]float64{
		2.26987774529616870924e-17, // 0x3C7A2B7F, 0x222F65E2
		3.06161699786838301793e-17, // 0x3C81A626, 0x33145C07
		1.39033110312309984516e-17, // 0x3C700788, 0x7AF0CBBD
		6.12323399573676603587e-17, // 0x3C91A626, 0x33145C07
	}
	atanCoefficients = [11]float64{
		3.33333333333329318027e-01,  // 0x3FD55555, 0x5555550D
		-1.99999999998764832476e-01, // 0xBFC99999, 0x9998EBC4
		1.42857142725034663711e-01,  // 0x3FC24924, 0x920083FF
		-1.11111104054623557880e-01, // 0xBFBC71C6, 0xFE231671
		9.09088713343650656196e-02,  // 0x3FB745CD, 0xC54C206E
		-7.69187620504482999495e-02, // 0xBFB3B0F2, 0xAF749A6D
		6.66107313738753120669e-02,  // 0x3FB10D66, 0xA0D03D51
		-5.83357013379057348645e-02, // 0xBFADDE2D, 0x52DEFD9A
		4.97687799461593236017e-02,  // 0x3FA97B4B, 0x24760DEB
		-3.65315727442169155270e-02, // 0xBFA2B444, 0x2C6A6C2F
		1.62858201153657823623e-02,  // 0x3F90AD3A, 0xE322DA11
	}
)

func atan(x float64) float64 {
	aT := &atanCoefficients
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	if ix >= 0x44100000 { // |x| >= 2^66
		if ix > 0x7FF00000 || ix == 0x7FF00000 && lowWord(x) != 0 {
			return x + x // NaN
		}
		if hx > 0 {
			return atanHi[3] + atanLo[3]
		}
		return -atanHi[3] - atanLo[3]
	}
	id := -1
	if ix < 0x3FDC0000 { // |x| < 0.4375
		if ix < 0x3E400000 { // |x| < 2^-27
			return x
		}
	} else {
		x = math.Abs(x)
		switch {
		case ix < 0x3FE60000: // 7/16 <= |x| < 11/16
			id = 0
			x = (float64(2.0*x) - 1) / (2.0 + x)
		case ix < 0x3FF30000: // 11/16 <= |x| < 19/16
			id = 1
			x = (x - 1) / (x + 1)
		case ix < 0x40038000: // |x| < 2.4375
			id = 2
			x = (x - 1.5) / (1 + float64(1.5*x))
		default: // 2.4375 <= |x| < 2^66
			id = 3
			x = -1.0 / x
		}
	}
	z := float64(x * x)
	w := float64(z * z)
	// the sum of aT[i] z^(i+1), split into its odd and even terms
	s1 := float64(z * (aT[0] + float64(w*(aT[2]+float64(w*(aT[4]+float64(w*(aT[6]+float64(w*(aT[8]+float64(w*aT[10])))))))))))
	s2 := float64(w * (aT[1] + float64(w*(aT[3]+float64(w*(aT[5]+float64(w*(aT[7]+float64(w*aT[9])))))))))
	if id < 0 {
		return x - float64(x*(s1+s2))
	}
	z = atanHi[id] - ((float64(x*(s1+s2)) - atanLo[id]) - x)
	if hx < 0 {
		return -z
	}
	return z
}

func atan2(y, x float64) float64 {
	const (
		piOver4 = 7.8539816339744827900e-01 // 0x3FE921FB, 0x54442D18
		piOver2 = 1.5707963267948965580e+00 // 0x3FF921FB, 0x54442D18
		pi      = 3.1415926535897931160e+00 // 0x400921FB, 0x54442D18
		piLo    = 1.2246467991473531772e-16 // 0x3CA1A626, 0x33145C07
	)
	if x != x || y != y {
		return x + y
	}
	hx, lx := highWord(x), lowWord(x)
	hy, ly := highWord(y), lowWord(y)
	ix, iy := hx&0x7FFFFFFF, hy&0x7FFFFFFF
	if uint32(hx-0x3FF00000)|lx == 0 { // x is 1
		return atan(y)
	}
	m := (hy>>31)&1 | (hx>>30)&2 // 2*sign(x) + sign(y)
	if uint32(iy)|ly == 0 {      // y is 0
		switch m {
		case 0, 1:
			return y // atan(±0, +anything) = ±0
		case 2:
			return pi // atan(+0, -anything) = pi
		}
		return -pi // atan(-0, -anything) = -pi
	}
	if uint32(ix)|lx == 0 { // x is 0
		if hy < 0 {
			return -piOver2
		}
		return piOver2
	}
	if ix == 0x7FF00000 { // x is infinite
		if iy == 0x7FF00000 {
			switch m {
			case 0:
				return piOver4
			case 1:
				return -piOver4
			case 2:
				return float64(3.0 * piOver4)
			}
			return float64(-3.0 * piOver4)
		}
		switch m {
		case 0:
			return 0
		case 1:
			return math.Copysign(0, -1)
		case 2:
			return pi
		}
		return -pi
	}
	if iy == 0x7FF00000 { // y is infinite
		if hy < 0 {
			return -piOver2
		}
		return piOver2
	}
	var z float64
	switch k := (iy - ix) >> 20; {
	case k > 60: // |y/x| > 2^60
		z = piOver2 + float64(0.5*piLo)
		m &= 1
	case hx < 0 && k < -60: // 0 > |y|/x > -2^-60
		z = 0
	default:
		z = atan(math.Abs(y / x))
	}
	switch m {
	case 0:
		return z
	case 1:
		return -z
	case 2:
		return pi - (z - piLo)
	}
	return (z - piLo) - pi
}

// ---- exp, expm1, log, log1p, log2, log10 ----

const (
	ln2Hi  = 6.93147180369123816490e-01 // 0x3FE62E42, 0xFEE00000
	ln2Lo  = 1.90821492927058770002e-10 // 0x3DEA39EF, 0x35793C76
	invLn2 = 1.44269504088896338700e+00 // 0x3FF71547, 0x652B82FE
	two54  = 1.80143985094819840000e+16 // 0x43500000, 0x00000000
	// the polynomial of log and log1p (Lg1 to Lg7, Lp1 to Lp7)
	lg1 = 6.666666666666735130e-01 // 3FE55555 55555593
	lg2 = 3.999999999940941908e-01 // 3FD99999 9997FA04
	lg3 = 2.857142874366239149e-01 // 3FD24924 94229359
	lg4 = 2.222219843214978396e-01 // 3FCC71C5 1D8E78AF
	lg5 = 1.818357216161805012e-01 // 3FC74664 96CB03DE
	lg6 = 1.531383769920937332e-01 // 3FC39A09 D078C69F
	lg7 = 1.479819860511658591e-01 // 3FC2F112 DF3E5244
)

func exp(x float64) float64 {
	const (
		oThreshold = 7.09782712893383973096e+02  // 0x40862E42, 0xFEFA39EF
		uThreshold = -7.45133219101941108420e+02 // 0xC0874910, 0xD52D3051
		P1         = 1.66666666666666019037e-01  // 0x3FC55555, 0x5555553E
		P2         = -2.77777777770155933842e-03 // 0xBF66C16C, 0x16BEBD93
		P3         = 6.61375632143793436117e-05  // 0x3F11566A, 0xAF25DE2C
		P4         = -1.65339022054652515390e-06 // 0xBEBBBD41, 0xC5D26BF1
		P5         = 4.13813679705723846039e-08  // 0x3E663769, 0x72BEA4D0
		e          = 2.718281828459045           // 0x4005BF0A, 0x8B145769
		twom1000   = 9.33263618503218878990e-302 // 2^-1000
		two1023    = 8.988465674311579539e307    // 2^1023
	)
	hx := uint32(highWord(x))
	negative := hx>>31 != 0
	hx &= 0x7FFFFFFF
	if hx >= 0x40862E42 { // |x| >= 709.78…
		if hx >= 0x7FF00000 {
			if hx&0xFFFFF|lowWord(x) != 0 {
				return x + x // NaN
			}
			if negative {
				return 0 // exp(-inf)
			}
			return x
		}
		if x > oThreshold {
			return math.Inf(1)
		}
		if x < uThreshold {
			return 0
		}
	}
	var hi, lo float64
	k := int32(0)
	switch {
	case hx > 0x3FD62E42: // |x| > 0.5 ln2
		if hx < 0x3FF0A2B2 { // and |x| < 1.5 ln2
			if x == 1 {
				return e // V8 returns e itself, which the reduction below misses by a bit
			}
			if negative {
				hi, lo, k = x+ln2Hi, -ln2Lo, -1
			} else {
				hi, lo, k = x-ln2Hi, ln2Lo, 1
			}
		} else {
			half := 0.5
			if negative {
				half = -0.5
			}
			k = int32(float64(invLn2*x) + half)
			t := float64(k)
			hi = x - float64(t*ln2Hi) // exact
			lo = float64(t * ln2Lo)
		}
		x = hi - lo
	case hx < 0x3E300000: // |x| < 2^-28
		return 1 + x
	}
	t := float64(x * x)
	var twopk float64
	if k >= -1021 {
		twopk = fromWords(0x3FF00000+k<<20, 0)
	} else {
		twopk = fromWords(0x3FF00000+(k+1000)<<20, 0)
	}
	c := x - float64(t*(P1+float64(t*(P2+float64(t*(P3+float64(t*(P4+float64(t*P5)))))))))
	if k == 0 {
		return 1 - (float64(x*c)/(c-2.0) - x)
	}
	y := 1 - ((lo - float64(x*c)/(2.0-c)) - hi)
	if k >= -1021 {
		if k == 1024 {
			return float64(float64(y*2.0) * two1023)
		}
		return float64(y * twopk)
	}
	return float64(float64(y*twopk) * twom1000)
}

func expm1(x float64) float64 {
	const (
		oThreshold = 7.09782712893383973096e+02 // 0x40862E42, 0xFEFA39EF
		// the scaled Q of R(2z), z = x*x/2
		Q1 = -3.33333333333331316428e-02 // BFA11111 111110F4
		Q2 = 1.58730158725481460165e-03  // 3F5A01A0 19FE5585
		Q3 = -7.93650757867487942473e-05 // BF14CE19 9EAADBB7
		Q4 = 4.00821782732936239552e-06  // 3ED0CFCA 86E65239
		Q5 = -2.01099218183624371326e-07 // BE8AFDB7 6E09C32D
	)
	hx := uint32(highWord(x))
	negative := hx&0x80000000 != 0
	hx &= 0x7FFFFFFF
	if hx >= 0x4043687A { // |x| >= 56 ln2
		if hx >= 0x40862E42 { // |x| >= 709.78…
			if hx >= 0x7FF00000 {
				if hx&0xFFFFF|lowWord(x) != 0 {
					return x + x // NaN
				}
				if negative {
					return -1
				}
				return x
			}
			if x > oThreshold {
				return math.Inf(1)
			}
		}
		if negative { // x < -56 ln2
			return -1
		}
	}
	var hi, lo, c float64
	k := int32(0)
	switch {
	case hx > 0x3FD62E42: // |x| > 0.5 ln2
		if hx < 0x3FF0A2B2 { // and |x| < 1.5 ln2
			if negative {
				hi, lo, k = x+ln2Hi, -ln2Lo, -1
			} else {
				hi, lo, k = x-ln2Hi, ln2Lo, 1
			}
		} else {
			half := 0.5
			if negative {
				half = -0.5
			}
			k = int32(float64(invLn2*x) + half)
			t := float64(k)
			hi = x - float64(t*ln2Hi) // exact
			lo = float64(t * ln2Lo)
		}
		x = hi - lo
		c = (hi - x) - lo
	case hx < 0x3C900000: // |x| < 2^-54
		return x
	}
	hfx := float64(0.5 * x)
	hxs := float64(x * hfx)
	r1 := 1 + float64(hxs*(Q1+float64(hxs*(Q2+float64(hxs*(Q3+float64(hxs*(Q4+float64(hxs*Q5)))))))))
	t := 3.0 - float64(r1*hfx)
	e := float64(hxs * ((r1 - t) / (6.0 - float64(x*t))))
	if k == 0 {
		return x - (float64(x*e) - hxs)
	}
	twopk := fromWords(0x3FF00000+k<<20, 0)
	e = float64(x*(e-c)) - c
	e -= hxs
	switch {
	case k == -1:
		return float64(0.5*(x-e)) - 0.5
	case k == 1:
		if x < -0.25 {
			return float64(-2.0 * (e - (x + 0.5)))
		}
		return 1 + float64(2.0*(x-e))
	case k <= -2 || k > 56: // exp(x) - 1 is enough
		y := 1 - (e - x)
		if k == 1024 {
			y = float64(float64(y*2.0) * 8.98846567431158e+307)
		} else {
			y = float64(y * twopk)
		}
		return y - 1
	case k < 20:
		t := fromWords(0x3FF00000-(0x200000>>k), 0) // 1 - 2^-k
		y := t - (e - x)
		return float64(y * twopk)
	}
	t = fromWords((0x3FF-k)<<20, 0) // 2^-k
	y := x - (e + t)
	y += 1
	return float64(y * twopk)
}

func log(x float64) float64 {
	hx, lx := highWord(x), lowWord(x)
	k := int32(0)
	if hx < 0x00100000 { // x < 2^-1022
		if uint32(hx&0x7FFFFFFF)|lx == 0 {
			return math.Inf(-1) // log(±0)
		}
		if hx < 0 {
			return math.NaN() // log of a negative number
		}
		k -= 54
		x *= two54 // a subnormal number, scaled up
		hx = highWord(x)
	}
	if hx >= 0x7FF00000 {
		return x + x
	}
	k += (hx >> 20) - 1023
	hx &= 0x000FFFFF
	i := (hx + 0x95F64) & 0x100000
	x = withHighWord(x, hx|(i^0x3FF00000)) // normalize x or x/2
	k += i >> 20
	f := x - 1.0
	if 0x000FFFFF&(2+hx) < 3 { // -2^-20 <= f < 2^-20
		if f == 0 {
			if k == 0 {
				return 0
			}
			dk := float64(k)
			return float64(dk*ln2Hi) + float64(dk*ln2Lo)
		}
		R := float64(float64(f*f) * (0.5 - float64(0.33333333333333333*f)))
		if k == 0 {
			return f - R
		}
		dk := float64(k)
		return float64(dk*ln2Hi) - ((R - float64(dk*ln2Lo)) - f)
	}
	s := f / (2.0 + f)
	dk := float64(k)
	z := float64(s * s)
	i = hx - 0x6147A
	w := float64(z * z)
	j := 0x6B851 - hx
	t1 := float64(w * (lg2 + float64(w*(lg4+float64(w*lg6)))))
	t2 := float64(z * (lg1 + float64(w*(lg3+float64(w*(lg5+float64(w*lg7)))))))
	i |= j
	R := t2 + t1
	if i > 0 {
		hfsq := float64(float64(0.5*f) * f)
		if k == 0 {
			return f - (hfsq - float64(s*(hfsq+R)))
		}
		return float64(dk*ln2Hi) - ((hfsq - (float64(s*(hfsq+R)) + float64(dk*ln2Lo))) - f)
	}
	if k == 0 {
		return f - float64(s*(f-R))
	}
	return float64(dk*ln2Hi) - ((float64(s*(f-R)) - float64(dk*ln2Lo)) - f)
}

func log1p(x float64) float64 {
	hx := highWord(x)
	ax := hx & 0x7FFFFFFF
	k := int32(1)
	var f, c float64
	var hu int32
	if hx < 0x3FDA827A { // 1 + x < sqrt(2)+
		if ax >= 0x3FF00000 { // x <= -1
			if x == -1.0 {
				return math.Inf(-1)
			}
			return math.NaN()
		}
		if ax < 0x3E200000 { // |x| < 2^-29
			if ax < 0x3C900000 { // |x| < 2^-54
				return x
			}
			return x - float64(float64(x*x)*0.5)
		}
		if hx > 0 || hx <= int32(-0x402D413C) { // sqrt(2)/2- <= 1 + x < sqrt(2)+ (hx <= 0xBFD2BEC4)
			k = 0
			f = x
			hu = 1
		}
	}
	if hx >= 0x7FF00000 {
		return x + x
	}
	if k != 0 {
		var u float64
		if hx < 0x43400000 {
			u = 1.0 + x
			hu = highWord(u)
			k = (hu >> 20) - 1023
			if k > 0 { // the correction term
				c = 1.0 - (u - x)
			} else {
				c = x - (u - 1.0)
			}
			c /= u
		} else {
			u = x
			hu = highWord(u)
			k = (hu >> 20) - 1023
			c = 0
		}
		hu &= 0x000FFFFF
		if hu < 0x6A09E { // u ~< sqrt(2)
			u = withHighWord(u, hu|0x3FF00000) // normalize u
		} else {
			k++
			u = withHighWord(u, hu|0x3FE00000) // normalize u/2
			hu = (0x00100000 - hu) >> 2
		}
		f = u - 1.0
	}
	hfsq := float64(float64(0.5*f) * f)
	dk := float64(k)
	if hu == 0 { // |f| < 2^-20
		if f == 0 {
			if k == 0 {
				return 0
			}
			c += float64(dk * ln2Lo)
			return float64(dk*ln2Hi) + c
		}
		R := float64(hfsq * (1.0 - float64(0.66666666666666666*f)))
		if k == 0 {
			return f - R
		}
		return float64(dk*ln2Hi) - ((R - (float64(dk*ln2Lo) + c)) - f)
	}
	s := f / (2.0 + f)
	z := float64(s * s)
	R := float64(z * (lg1 + float64(z*(lg2+float64(z*(lg3+float64(z*(lg4+float64(z*(lg5+float64(z*(lg6+float64(z*lg7)))))))))))))
	if k == 0 {
		return f - (hfsq - float64(s*(hfsq+R)))
	}
	return float64(dk*ln2Hi) - ((hfsq - (float64(s*(hfsq+R)) + (float64(dk*ln2Lo) + c))) - f)
}

// kernelLog1p is FreeBSD's k_log1p: log(1 + f) - f for 1 + f in ~[sqrt(2)/2, sqrt(2)].
func kernelLog1p(f float64) float64 {
	s := f / (2.0 + f)
	z := float64(s * s)
	w := float64(z * z)
	t1 := float64(w * (lg2 + float64(w*(lg4+float64(w*lg6)))))
	t2 := float64(z * (lg1 + float64(w*(lg3+float64(w*(lg5+float64(w*lg7)))))))
	R := t2 + t1
	hfsq := float64(float64(0.5*f) * f)
	return float64(s * (hfsq + R))
}

// log2 is FreeBSD's e_log2.c: log(x) split as k + log(1 + f), with f - f²/2 in extra precision so a
// power of two comes out exact.
func log2(x float64) float64 {
	const (
		ivln2hi = 1.44269504072144627571e+00 // 0x3FF71547, 0x65200000
		ivln2lo = 1.67517131648865118353e-10 // 0x3DE705FC, 0x2EEFA200
	)
	hx, lx := highWord(x), lowWord(x)
	k := int32(0)
	if hx < 0x00100000 { // x < 2^-1022
		if uint32(hx&0x7FFFFFFF)|lx == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= two54
		hx = highWord(x)
	}
	if hx >= 0x7FF00000 {
		return x + x
	}
	if hx == 0x3FF00000 && lx == 0 {
		return 0 // log2(1) = +0
	}
	k += (hx >> 20) - 1023
	hx &= 0x000FFFFF
	i := (hx + 0x95F64) & 0x100000
	x = withHighWord(x, hx|(i^0x3FF00000)) // normalize x or x/2
	k += i >> 20
	y := float64(k)
	f := x - 1.0
	hfsq := float64(float64(0.5*f) * f)
	r := kernelLog1p(f)
	hi := withLowWord(f-hfsq, 0)
	lo := (f - hi) - hfsq + r
	valueHi := float64(hi * ivln2hi)
	valueLo := float64((lo+hi)*ivln2lo) + float64(lo*ivln2hi)
	w := y + valueHi
	valueLo += (y - w) + valueHi
	valueHi = w
	return valueLo + valueHi
}

// log10 is fdlibm's e_log10.c: n*log10(2) + log(x)/ln(10), with x scaled into [1, 2).
func log10(x float64) float64 {
	const (
		ivln10    = 4.34294481903251816668e-01 // 0x3FDBCB7B, 0x1526E50E
		log10_2hi = 3.01029995663611771306e-01 // 0x3FD34413, 0x509F6000
		log10_2lo = 3.69423907715893078616e-13 // 0x3D59FEF3, 0x11F12B36
	)
	hx, lx := highWord(x), lowWord(x)
	k := int32(0)
	if hx < 0x00100000 { // x < 2^-1022
		if uint32(hx&0x7FFFFFFF)|lx == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= two54
		hx = highWord(x)
		lx = lowWord(x)
	}
	if hx >= 0x7FF00000 {
		return x + x
	}
	if hx == 0x3FF00000 && lx == 0 {
		return 0 // log10(1) = +0
	}
	k += (hx >> 20) - 1023
	i := int32(uint32(k) >> 31) // 1 when k is negative
	hx = (hx & 0x000FFFFF) | ((0x3FF - i) << 20)
	y := float64(k + i)
	x = fromWords(hx, lx)
	z := float64(y*log10_2lo) + float64(ivln10*log(x))
	return z + float64(y*log10_2hi)
}

// ---- cbrt, sinh, cosh, tanh, asinh, acosh, atanh ----

// cbrt is FreeBSD's s_cbrt.c: a rough cube root from the exponent, a polynomial to 23 bits, then one
// Newton step to 53.
func cbrt(x float64) float64 {
	const (
		B1 = 715094163 // (1023 - 1023/3 - 0.03306235651) * 2^20
		B2 = 696219795 // (1023 - 1023/3 - 54/3 - 0.03306235651) * 2^20
		// |1/cbrt(x) - p(x)| < 2^-23.5
		P0 = 1.87595182427177009643   // 0x3FFE03E6, 0x0F61E692
		P1 = -1.88497979543377169875  // 0xBFFE28E0, 0x92F02420
		P2 = 1.621429720105354466140  // 0x3FF9F160, 0x4A49D6C2
		P3 = -0.758397934778766047437 // 0xBFE844CB, 0xBEE751D9
		P4 = 0.145996192886612446982  // 0x3FC2B000, 0xD4E4EDD7
	)
	hx := uint32(highWord(x))
	sign := hx & 0x80000000
	hx ^= sign
	if hx >= 0x7FF00000 {
		return x + x // NaN and the infinities are themselves
	}
	var t float64
	if hx < 0x00100000 { // zero or subnormal
		if hx|lowWord(x) == 0 {
			return x
		}
		t = float64(fromWords(0x43500000, 0) * x) // x * 2^54
		high := uint32(highWord(t))
		t = fromWords(int32(sign|((high&0x7FFFFFFF)/3+B2)), 0)
	} else {
		t = fromWords(int32(sign|(hx/3+B1)), 0)
	}
	r := float64(float64(t*t) * (t / x))
	t = float64(t * (float64(P0+float64(r*(P1+float64(r*P2)))) + float64(float64(float64(r*r)*r)*(P3+float64(r*P4)))))
	// round t away from zero to 23 bits
	t = math.Float64frombits((math.Float64bits(t) + 0x80000000) & 0xFFFFFFFFC0000000)
	s := float64(t * t) // exact
	r = x / s
	w := t + t // exact
	r = (r - t) / (w + r)
	return t + float64(t*r)
}

func sinh(x float64) float64 {
	const (
		overflow = 710.4758600739439
		twoM28   = 3.725290298461914e-9 // 2^-28
		logMax   = 709.7822265625       // 0x40862E42, 0x00000000
	)
	h := 0.5
	if x < 0 {
		h = -0.5
	}
	ax := math.Abs(x)
	switch {
	case ax < 22: // sign(x) * 0.5 * (E + E/(E + 1))
		if ax < twoM28 {
			return x
		}
		t := expm1(ax)
		if ax < 1 {
			return float64(h * (float64(2.0*t) - float64(t*t)/(t+1.0)))
		}
		return float64(h * (t + t/(t+1.0)))
	case ax < logMax:
		return float64(h * exp(ax))
	case ax <= overflow:
		w := exp(float64(0.5 * ax))
		t := float64(h * w)
		return float64(t * w)
	}
	return float64(x * 1.0e307) // past the overflow threshold, or NaN
}

func cosh(x float64) float64 {
	const overflow = 710.4758600739439
	ix := highWord(x) & 0x7FFFFFFF
	switch {
	case ix < 0x3FD62E43: // |x| in [0, 0.5 ln2]: 1 + expm1(|x|)^2 / (2 exp(|x|))
		t := expm1(math.Abs(x))
		w := 1 + t
		if ix < 0x3C800000 { // |x| < 2^-55
			return w
		}
		return 1 + float64(t*t)/(w+w)
	case ix < 0x40360000: // |x| in [0.5 ln2, 22]: (exp(|x|) + 1/exp(|x|)) / 2
		t := exp(math.Abs(x))
		return float64(0.5*t) + 0.5/t
	case ix < 0x40862E42: // |x| in [22, log(maxdouble)]
		return float64(0.5 * exp(math.Abs(x)))
	case math.Abs(x) <= overflow:
		w := exp(float64(0.5 * math.Abs(x)))
		t := float64(0.5 * w)
		return float64(t * w)
	case ix >= 0x7FF00000: // infinity or NaN
		return float64(x * x)
	}
	return math.Inf(1)
}

func tanh(x float64) float64 {
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	if ix >= 0x7FF00000 { // infinity or NaN
		if hx >= 0 {
			return 1/x + 1
		}
		return 1/x - 1
	}
	var z float64
	switch {
	case ix >= 0x40360000: // |x| >= 22
		z = 1
	case ix < 0x3E300000: // |x| < 2^-28
		return x
	case ix >= 0x3FF00000: // |x| >= 1
		t := expm1(float64(2.0 * math.Abs(x)))
		z = 1 - 2.0/(t+2.0)
	default:
		t := expm1(float64(-2.0 * math.Abs(x)))
		z = -t / (t + 2.0)
	}
	if hx >= 0 {
		return z
	}
	return -z
}

func asinh(x float64) float64 {
	const ln2 = 6.93147180559945286227e-01 // 0x3FE62E42, 0xFEFA39EF
	hx := highWord(x)
	ix := hx & 0x7FFFFFFF
	if ix >= 0x7FF00000 {
		return x + x // infinity or NaN
	}
	if ix < 0x3E300000 { // |x| < 2^-28
		return x
	}
	var w float64
	switch {
	case ix > 0x41B00000: // |x| > 2^28
		w = log(math.Abs(x)) + ln2
	case ix > 0x40000000: // 2^28 > |x| > 2
		t := math.Abs(x)
		w = log(float64(2.0*t) + 1/(math.Sqrt(float64(x*x)+1)+t))
	default: // 2 > |x| > 2^-28
		t := float64(x * x)
		w = log1p(math.Abs(x) + t/(1+math.Sqrt(1+t)))
	}
	if hx > 0 {
		return w
	}
	return -w
}

func acosh(x float64) float64 {
	const ln2 = 6.93147180559945286227e-01 // 0x3FE62E42, 0xFEFA39EF
	hx, lx := highWord(x), lowWord(x)
	switch {
	case hx < 0x3FF00000: // x < 1
		return (x - x) / (x - x)
	case hx >= 0x41B00000: // x > 2^28
		if hx >= 0x7FF00000 {
			return x + x // infinity or NaN
		}
		return log(x) + ln2
	case uint32(hx-0x3FF00000)|lx == 0:
		return 0 // acosh(1)
	case hx > 0x40000000: // 2^28 > x > 2
		t := float64(x * x)
		return log(float64(2.0*x) - 1/(x+math.Sqrt(t-1)))
	}
	t := x - 1 // 1 < x < 2
	return log1p(t + math.Sqrt(float64(2.0*t)+float64(t*t)))
}

func atanh(x float64) float64 {
	hx, lx := highWord(x), lowWord(x)
	ix := hx & 0x7FFFFFFF
	switch {
	case x != x || uint32(ix)|(lx|-lx)>>31 > 0x3FF00000: // |x| > 1
		return (x - x) / (x - x)
	case ix == 0x3FF00000:
		return x / 0 // ±Infinity
	case ix < 0x3E300000: // |x| < 2^-28
		return x
	}
	x = withHighWord(x, ix)
	var t float64
	if ix < 0x3FE00000 { // |x| < 0.5
		t = x + x
		t = float64(0.5 * log1p(t+float64(t*x)/(1-x)))
	} else {
		t = float64(0.5 * log1p((x+x)/(1-x)))
	}
	if hx >= 0 {
		return t
	}
	return -t
}

// ---- pow ----

// pow is fdlibm's e_pow.c: log2(x) in extra precision, times y in extra precision, then 2 to that.
// JavaScript's NaN for 1 or -1 to an infinite power is fdlibm's own answer.
func pow(x, y float64) float64 {
	const (
		two53   = 9007199254740992.0
		huge    = 1.0e300
		tiny    = 1.0e-300
		L1      = 5.99999999999994648725e-01  // 0x3FE33333, 0x33333303
		L2      = 4.28571428578550184252e-01  // 0x3FDB6DB6, 0xDB6FABFF
		L3      = 3.33333329818377432918e-01  // 0x3FD55555, 0x518F264D
		L4      = 2.72728123808534006489e-01  // 0x3FD17460, 0xA91D4101
		L5      = 2.30660745775561754067e-01  // 0x3FCD864A, 0x93C9DB65
		L6      = 2.06975017800338417784e-01  // 0x3FCA7E28, 0x4A454EEF
		P1      = 1.66666666666666019037e-01  // 0x3FC55555, 0x5555553E
		P2      = -2.77777777770155933842e-03 // 0xBF66C16C, 0x16BEBD93
		P3      = 6.61375632143793436117e-05  // 0x3F11566A, 0xAF25DE2C
		P4      = -1.65339022054652515390e-06 // 0xBEBBBD41, 0xC5D26BF1
		P5      = 4.13813679705723846039e-08  // 0x3E663769, 0x72BEA4D0
		lg2     = 6.93147180559945286227e-01  // 0x3FE62E42, 0xFEFA39EF
		lg2_h   = 6.93147182464599609375e-01  // 0x3FE62E43, 0x00000000
		lg2_l   = -1.90465429995776804525e-09 // 0xBE205C61, 0x0CA86C39
		ovt     = 8.0085662595372944372e-17   // -(1024 - log2(ovfl + .5ulp))
		cp      = 9.61796693925975554329e-01  // 0x3FEEC709, 0xDC3A03FD = 2/(3 ln2)
		cp_h    = 9.61796700954437255859e-01  // 0x3FEEC709, 0xE0000000 = (float)cp
		cp_l    = -7.02846165095275826516e-09 // 0xBE3E2FE0, 0x145B01F5 = tail of cp_h
		ivln2   = 1.44269504088896338700e+00  // 0x3FF71547, 0x652B82FE = 1/ln2
		ivln2_h = 1.44269502162933349609e+00  // 0x3FF71547, 0x60000000 = 24 bits of 1/ln2
		ivln2_l = 1.92596299112661746887e-08  // 0x3E54AE0B, 0xF85DDF44 = tail of 1/ln2
	)
	bp := [2]float64{1.0, 1.5}
	dpH := [2]float64{0.0, 5.84962487220764160156e-01} // 0x3FE2B803, 0x40000000
	dpL := [2]float64{0.0, 1.35003920212974897128e-08} // 0x3E4CFDEB, 0x43CFD006

	hx, lx := highWord(x), lowWord(x)
	hy, ly := highWord(y), lowWord(y)
	ix, iy := hx&0x7FFFFFFF, hy&0x7FFFFFFF

	if uint32(iy)|ly == 0 { // x**0 = 1
		return 1
	}
	if x != x || y != y {
		return x + y
	}
	// yIsInteger: 0 when y is not an integer, 1 an odd integer, 2 an even one (read when x < 0)
	yIsInteger := int32(0)
	if hx < 0 {
		switch {
		case iy >= 0x43400000:
			yIsInteger = 2
		case iy >= 0x3FF00000:
			k := (iy >> 20) - 0x3FF // exponent
			if k > 20 {
				j := ly >> (52 - k)
				if j<<(52-k) == ly {
					yIsInteger = 2 - int32(j&1)
				}
			} else if ly == 0 {
				j := iy >> (20 - k)
				if j<<(20-k) == iy {
					yIsInteger = 2 - j&1
				}
			}
		}
	}
	// y is ±infinity, ±1, 2 or 0.5
	if ly == 0 {
		switch {
		case iy == 0x7FF00000:
			switch {
			case uint32(ix-0x3FF00000)|lx == 0:
				return y - y // (±1)**±inf is NaN
			case ix >= 0x3FF00000: // (|x| > 1)**±inf = inf, 0
				if hy >= 0 {
					return y
				}
				return 0
			}
			if hy < 0 { // (|x| < 1)**±inf = 0, inf
				return -y
			}
			return 0
		case iy == 0x3FF00000:
			if hy < 0 {
				return 1 / x
			}
			return x
		case hy == 0x40000000:
			return float64(x * x)
		case hy == 0x3FE00000 && hx >= 0: // x >= +0 to the 0.5
			return math.Sqrt(x)
		}
	}
	ax := math.Abs(x)
	// x is ±0, ±inf or ±1
	if lx == 0 && (ix == 0x7FF00000 || ix == 0 || ix == 0x3FF00000) {
		z := ax
		if hy < 0 {
			z = 1 / z
		}
		if hx < 0 {
			if (ix-0x3FF00000)|yIsInteger == 0 {
				z = math.NaN() // (-1)**non-integer
			} else if yIsInteger == 1 {
				z = -z // (x < 0)**odd = -(|x|**odd)
			}
		}
		return z
	}
	positive := hx >= 0
	if !positive && yIsInteger == 0 { // (x < 0)**non-integer is NaN
		return math.NaN()
	}
	s := 1.0 // the sign of the result
	if !positive && yIsInteger == 1 {
		s = -1
	}
	var t1, t2 float64
	if iy > 0x41E00000 { // |y| > 2^31
		if iy > 0x43F00000 { // |y| > 2^64: over or underflow
			if ix <= 0x3FEFFFFF {
				if hy < 0 {
					return math.Inf(1)
				}
				return 0
			}
			if ix >= 0x3FF00000 {
				if hy > 0 {
					return math.Inf(1)
				}
				return 0
			}
		}
		// over or underflow when x is not close to 1
		if ix < 0x3FEFFFFF {
			if hy < 0 {
				return float64(float64(s*huge) * huge)
			}
			return float64(float64(s*tiny) * tiny)
		}
		if ix > 0x3FF00000 {
			if hy > 0 {
				return float64(float64(s*huge) * huge)
			}
			return float64(float64(s*tiny) * tiny)
		}
		// |1 - x| <= 2^-20: log(x) by x - x^2/2 + x^3/3 - x^4/4
		t := ax - 1
		w := float64(float64(t*t) * (0.5 - float64(t*(0.3333333333333333333333-float64(t*0.25)))))
		u := float64(ivln2_h * t)
		v := float64(t*ivln2_l) - float64(w*ivln2)
		t1 = withLowWord(u+v, 0)
		t2 = v - (t1 - u)
	} else {
		n := int32(0)
		if ix < 0x00100000 { // a subnormal x
			ax *= two53
			n -= 53
			ix = highWord(ax)
		}
		n += (ix >> 20) - 0x3FF
		j := ix & 0x000FFFFF
		ix = j | 0x3FF00000 // normalize ix
		var k int32
		switch {
		case j <= 0x3988E: // |x| < sqrt(3/2)
			k = 0
		case j < 0xBB67A: // |x| < sqrt(3)
			k = 1
		default:
			k = 0
			n++
			ix -= 0x00100000
		}
		ax = withHighWord(ax, ix)
		// ss = s_h + s_l = (x - 1)/(x + 1) or (x - 1.5)/(x + 1.5)
		u := ax - bp[k]
		v := 1 / (ax + bp[k])
		ss := float64(u * v)
		sH := withLowWord(ss, 0)
		tH := fromWords(((ix>>1)|0x20000000)+0x00080000+(k<<18), 0) // ax + bp[k], high part
		tL := ax - (tH - bp[k])
		sL := float64(v * ((u - float64(sH*tH)) - float64(sH*tL)))
		// log(ax)
		s2 := float64(ss * ss)
		r := float64(float64(s2*s2) * (L1 + float64(s2*(L2+float64(s2*(L3+float64(s2*(L4+float64(s2*(L5+float64(s2*L6)))))))))))
		r += float64(sL * (sH + ss))
		s2 = float64(sH * sH)
		tH = withLowWord(3.0+s2+r, 0)
		tL = r - ((tH - 3.0) - s2)
		// u + v = ss (1 + …)
		u = float64(sH * tH)
		v = float64(sL*tH) + float64(tL*ss)
		// 2/(3 log2) (ss + …)
		pH := withLowWord(u+v, 0)
		pL := v - (pH - u)
		zH := float64(cp_h * pH) // cp_h + cp_l = 2/(3 log2)
		zL := float64(cp_l*pH) + float64(pL*cp) + dpL[k]
		// log2(ax) = (ss + …) 2/(3 log2) = n + dp_h + z_h + z_l
		t := float64(n)
		t1 = withLowWord(((zH+zL)+dpH[k])+t, 0)
		t2 = zL - (((t1 - t) - dpH[k]) - zH)
	}
	// (y1 + y2) * (t1 + t2), y split in two
	y1 := withLowWord(y, 0)
	pL := float64((y-y1)*t1) + float64(y*t2)
	pH := float64(y1 * t1)
	z := pL + pH
	j, i := highWord(z), lowWord(z)
	if j >= 0x40900000 { // z >= 1024
		if uint32(j-0x40900000)|i != 0 || pL+ovt > z-pH {
			return float64(float64(s*huge) * huge) // overflow
		}
	} else if j&0x7FFFFFFF >= 0x4090CC00 { // z <= -1075
		if uint32(j)-0xC090CC00|i != 0 || pL <= z-pH {
			return float64(float64(s*tiny) * tiny) // underflow
		}
	}
	// 2**(p_h + p_l)
	ii := j & 0x7FFFFFFF
	k := (ii >> 20) - 0x3FF
	n := int32(0)
	if ii > 0x3FE00000 { // |z| > 0.5: n = [z + 0.5]
		n = j + (0x00100000 >> (k + 1))
		k = ((n & 0x7FFFFFFF) >> 20) - 0x3FF // the new k for n
		t := fromWords(n&^(0x000FFFFF>>k), 0)
		n = ((n & 0x000FFFFF) | 0x00100000) >> (20 - k)
		if j < 0 {
			n = -n
		}
		pH -= t
	}
	t := withLowWord(pL+pH, 0)
	u := float64(t * lg2_h)
	v := float64((pL-(t-pH))*lg2) + float64(t*lg2_l)
	z = u + v
	w := v - (z - u)
	t = float64(z * z)
	t1 = z - float64(t*(P1+float64(t*(P2+float64(t*(P3+float64(t*(P4+float64(t*P5)))))))))
	r := float64(z*t1) / ((t1 - 2) - (w + float64(z*w)))
	z = 1 - (r - z)
	j = highWord(z)
	j += int32(uint32(n) << 20)
	if j>>20 <= 0 {
		z = math.Ldexp(z, int(n)) // a subnormal result
	} else {
		z = withHighWord(z, highWord(z)+int32(uint32(n)<<20))
	}
	return float64(s * z)
}
