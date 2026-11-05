package audioprocessing

import (
	"math"
	"math/cmplx"
)

const EQBandCount = 9

// EQFrequencies follows a perceptually even logarithmic distribution while
// retaining every centre frequency used by the original five-band equalizer.
var EQFrequencies = [EQBandCount]float64{120, 190, 300, 490, 800, 1260, 2000, 3160, 5000}

type biquad struct{ b0, b1, b2, a1, a2, z1, z2 float64 }

func (f *biquad) peak(sampleRate, frequency, gainDB float64) {
	a := math.Pow(10, gainDB/40)
	w := 2 * math.Pi * frequency / sampleRate
	alpha, cosine := math.Sin(w)/2, math.Cos(w)
	a0 := 1 + alpha/a
	f.b0, f.b1, f.b2 = (1+alpha*a)/a0, (-2*cosine)/a0, (1-alpha*a)/a0
	f.a1, f.a2 = (-2*cosine)/a0, (1-alpha/a)/a0
}

func (f *biquad) process(x float64) float64 {
	y := x*f.b0 + f.z1
	f.z1 = x*f.b1 + f.z2 - f.a1*y
	f.z2 = x*f.b2 - f.a2*y
	return y
}

type notchFilter struct {
	b0, b1, b2, a1, a2, z1, z2 float64
	tb0, tb1, tb2, ta1, ta2    float64
	mix, targetMix             float64
}

func (f *notchFilter) configure(sampleRate float64, c NotchConfig) {
	freq := clamp(c.FrequencyHz, 80, sampleRate*.45)
	width := clamp(c.WidthHz, 20, 2000)
	q := math.Max(freq/width, .25)
	w := 2 * math.Pi * freq / sampleRate
	alpha, cosine := math.Sin(w)/(2*q), math.Cos(w)
	a0 := 1 + alpha
	f.tb0, f.tb1, f.tb2 = 1/a0, -2*cosine/a0, 1/a0
	f.ta1, f.ta2 = f.tb1, (1-alpha)/a0
	if f.b0 == 0 {
		f.b0, f.b1, f.b2, f.a1, f.a2 = f.tb0, f.tb1, f.tb2, f.ta1, f.ta2
	}
	f.targetMix = 0
	if c.Enabled {
		depth := clamp(c.DepthDB, -60, -6)
		f.targetMix = 1 - math.Pow(10, depth/20)
	}
}

func (f *notchFilter) process(x float64) float64 {
	const smoothing = .002
	f.b0 += smoothing * (f.tb0 - f.b0)
	f.b1 += smoothing * (f.tb1 - f.b1)
	f.b2 += smoothing * (f.tb2 - f.b2)
	f.a1 += smoothing * (f.ta1 - f.a1)
	f.a2 += smoothing * (f.ta2 - f.a2)
	f.mix += smoothing * (f.targetMix - f.mix)
	y := x*f.b0 + f.z1
	f.z1 = x*f.b1 + f.z2 - f.a1*y
	f.z2 = x*f.b2 - f.a2*y
	return x + f.mix*(y-x)
}

type emphasisFilter struct{ previous, lowpass float64 }

func (f *emphasisFilter) process(x, sampleRate float64, mode EmphasisMode, microseconds int) float64 {
	if mode == EmphasisOff {
		f.previous, f.lowpass = x, x
		return x
	}
	if microseconds != 75 {
		microseconds = 50
	}
	alpha := 1 - math.Exp(-1/(float64(microseconds)*1e-6*sampleRate))
	f.lowpass += alpha * (x - f.lowpass)
	if mode == DeEmphasis {
		return f.lowpass
	}
	// Inverse of the one-pole de-emphasis, with a bounded Nyquist boost.
	y := f.lowpass + (x-f.previous)/math.Max(alpha, .05)
	f.previous = x
	return clamp(y, -4, 4)
}

func clamp(v, low, high float64) float64 { return math.Min(math.Max(v, low), high) }

// ResponseDB returns the combined linear-filter response used by the processor.
// Dynamic stages (noise reduction, compressor, limiter and output volume) are
// deliberately omitted because their response depends on the incoming signal.
func ResponseDB(c Config, sampleRate, frequency float64) float64 {
	if sampleRate <= 0 || frequency <= 0 || frequency >= sampleRate*.5 {
		return -80
	}
	z := cmplx.Exp(complex(0, -2*math.Pi*frequency/sampleRate))
	hpR := math.Exp(-2 * math.Pi * c.LowCutHz / sampleRate)
	h := (1 - z) / (1 - complex(hpR, 0)*z)
	lpA := 1 - math.Exp(-2*math.Pi*c.HighCutHz/sampleRate)
	lp := complex(lpA, 0) / (1 - complex(1-lpA, 0)*z)
	h *= lp * lp
	if c.EQEnabled {
		for i, center := range EQFrequencies {
			var f biquad
			f.peak(sampleRate, math.Min(center, sampleRate*.45), c.EQGainsDB[i])
			h *= (complex(f.b0, 0) + complex(f.b1, 0)*z + complex(f.b2, 0)*z*z) /
				(1 + complex(f.a1, 0)*z + complex(f.a2, 0)*z*z)
		}
	}
	if c.Notch.Enabled {
		var f notchFilter
		f.configure(sampleRate, c.Notch)
		notch := (complex(f.tb0, 0) + complex(f.tb1, 0)*z + complex(f.tb2, 0)*z*z) /
			(1 + complex(f.ta1, 0)*z + complex(f.ta2, 0)*z*z)
		h *= 1 + complex(f.targetMix, 0)*(notch-1)
	}
	if c.Emphasis != EmphasisOff {
		us := c.EmphasisMicroseconds
		if us != 75 {
			us = 50
		}
		a := 1 - math.Exp(-1/(float64(us)*1e-6*sampleRate))
		lowpass := complex(a, 0) / (1 - complex(1-a, 0)*z)
		if c.Emphasis == DeEmphasis {
			h *= lowpass
		} else {
			h *= lowpass + (1-z)/complex(math.Max(a, .05), 0)
		}
	} else if c.SSBClarityDB != 0 {
		a := 1 - math.Exp(-2*math.Pi*900/sampleRate)
		lowpass := complex(a, 0) / (1 - complex(1-a, 0)*z)
		lowGain := math.Pow(10, -c.SSBClarityDB/80)
		highGain := math.Pow(10, c.SSBClarityDB/20)
		h *= lowpass*complex(lowGain, 0) + (1-lowpass)*complex(highGain, 0)
	}
	return clamp(20*math.Log10(math.Max(cmplx.Abs(h), 1e-4)), -80, 24)
}
