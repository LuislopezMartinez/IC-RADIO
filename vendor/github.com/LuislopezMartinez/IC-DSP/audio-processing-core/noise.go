package audioprocessing

import (
	"math"
	"math/cmplx"
)

const noiseFFTSize = 512
const noiseHopSize = noiseFFTSize / 2

type noiseReducer struct {
	sampleRate                                                                 float64
	input                                                                      [noiseFFTSize]float32
	inputWrite                                                                 int
	overlap                                                                    [noiseHopSize]float64
	output                                                                     [noiseFFTSize * 4]float32
	outputRead, outputWrite, outputCount                                       int
	window                                                                     [noiseFFTSize]float64
	noisePower, smoothPower, minimum, previousMin, priorSNR, tonalMemory, gain [noiseFFTSize/2 + 1]float64
	frames, minimumAge                                                         int
	activity, reductionDB, signal                                              float32
	wet, broadGain, voice                                                      float64
}

type noiseResponse struct {
	threshold, span                 float64
	voiceAttack, voiceRelease       float64
	broadAttack, broadRelease       float64
	spectralAttack, spectralRelease float64
}

func responseForNoiseSpeed(speed NoiseSpeed) noiseResponse {
	// Slow deliberately matches the former Fast response. Normal and Fast
	// progressively shorten both the voice-gate and spectral-gain settling time.
	switch speed {
	case NoiseFast:
		return noiseResponse{3, 3, .88, .82, .92, .80, .82, .58}
	case NoiseNormal:
		return noiseResponse{3, 3, .62, .55, .70, .52, .55, .30}
	default:
		return noiseResponse{3, 3, .34, .30, .42, .22, .32, .12}
	}
}

func newNoiseReducer(sampleRate float64) noiseReducer {
	r := noiseReducer{sampleRate: sampleRate, broadGain: 1, inputWrite: noiseHopSize}
	for i := range r.window {
		r.window[i] = math.Sqrt(.5 - .5*math.Cos(2*math.Pi*float64(i)/noiseFFTSize))
	}
	for i := range r.gain {
		r.gain[i] = 1
	}
	return r
}

func (r *noiseReducer) status() NoiseStatus {
	return NoiseStatus{r.reductionDB, r.signal, r.frames < 94}
}

func (r *noiseReducer) process(samples []float32, enabled bool, strength float64, speed NoiseSpeed) {
	amount := clamp(strength, 0, 100) / 100
	if !enabled {
		amount = 0
	}
	for i, s := range samples {
		r.input[r.inputWrite] = s
		r.inputWrite++
		if r.inputWrite == noiseFFTSize {
			r.frame(amount, speed)
			copy(r.input[:noiseHopSize], r.input[noiseHopSize:])
			r.inputWrite = noiseHopSize
		}
		if r.outputCount == 0 {
			samples[i] = 0
			continue
		}
		samples[i] = r.output[r.outputRead]
		r.outputRead = (r.outputRead + 1) % len(r.output)
		r.outputCount--
	}
}

func (r *noiseReducer) frame(amount float64, speed NoiseSpeed) {
	response := responseForNoiseSpeed(speed)
	var bins [noiseFFTSize]complex128
	var power, target, prob [noiseFFTSize/2 + 1]float64
	var tonal [noiseFFTSize/2 + 1]bool
	for i, s := range r.input {
		bins[i] = complex(float64(s)*r.window[i], 0)
	}
	fft(bins[:], false)
	for i := range power {
		power[i] = cmplx.Abs(bins[i]) * cmplx.Abs(bins[i])
	}
	minGain := math.Pow(10, -(3+15*amount)/20)
	for bin := range power {
		current := power[bin]
		neighbour := 0.0
		n := 0
		for off := -6; off <= 6; off++ {
			c := bin + off
			if (off <= -3 || off >= 3) && c >= 0 && c < len(power) {
				neighbour += power[c]
				n++
			}
		}
		if n > 0 {
			neighbour /= float64(n)
		}
		if r.frames == 0 {
			r.noisePower[bin] = math.Max(current, 1e-16)
			r.smoothPower[bin] = current
			r.minimum[bin] = current
			r.previousMin[bin] = current
		} else {
			r.smoothPower[bin] += .16 * (current - r.smoothPower[bin])
			r.minimum[bin] = math.Min(r.minimum[bin], r.smoothPower[bin])
		}
		posterior := current / math.Max(r.noisePower[bin], 1e-16)
		r.priorSNR[bin] = .92*r.priorSNR[bin] + .08*math.Max(posterior-1, 0)
		prob[bin] = clamp((r.priorSNR[bin]-.15)/3.5, 0, 1)
		if neighbour > 0 && current > 5*neighbour {
			r.tonalMemory[bin] += .34 * (1 - r.tonalMemory[bin])
		} else {
			r.tonalMemory[bin] *= .25
		}
		if r.tonalMemory[bin] > .46 {
			prob[bin] = math.Max(prob[bin], .92)
			tonal[bin] = true
		}
		if r.frames > 0 {
			candidate := math.Max(1e-16, 2*math.Min(r.minimum[bin], r.previousMin[bin]))
			rate := .08 * (1 - .92*prob[bin])
			if candidate > r.noisePower[bin] {
				rate *= .1
			}
			r.noisePower[bin] += rate * (candidate - r.noisePower[bin])
		}
		w := r.priorSNR[bin] / (r.priorSNR[bin] + 1 + 1.15*amount)
		w = math.Max(minGain, math.Sqrt(math.Max(w, 0)))
		target[bin] = w + prob[bin]*.62*(1-w)
	}
	for bin, on := range tonal {
		if on {
			for off := -3; off <= 3; off++ {
				c := bin + off
				if c >= 0 && c < len(target) {
					target[c] = math.Max(target[c], .92)
				}
			}
		}
	}
	bandPower, bandNoise, tonalPresence := 0.0, 0.0, 0.0
	upper := min(len(power)-1, int(4000*noiseFFTSize/r.sampleRate))
	for bin := 1; bin <= upper; bin++ {
		bandPower += power[bin]
		bandNoise += r.noisePower[bin]
		if r.tonalMemory[bin] > .8 {
			tonalPresence = math.Max(tonalPresence, r.tonalMemory[bin])
		}
	}
	raw := math.Max(clamp((bandPower/math.Max(bandNoise, 1e-16)-response.threshold)/response.span, 0, 1), tonalPresence)
	rate := response.voiceRelease
	if raw > r.voice {
		rate = response.voiceAttack
	}
	r.voice += rate * (raw - r.voice)
	noiseOnly := math.Pow(10, -(2+6*amount)/20)
	targetBroad := noiseOnly + clamp((r.voice-.1)/.42, 0, 1)*(1-noiseOnly)
	rate = response.broadRelease
	if targetBroad > r.broadGain {
		rate = response.broadAttack
	}
	r.broadGain += rate * (targetBroad - r.broadGain)
	r.wet += .14 * (math.Min(1, amount*2) - r.wet)
	attenuation := 0.0
	weights := [5]float64{.1, .2, .4, .2, .1}
	for bin := range power {
		smoothed, weight := 0.0, 0.0
		for off := -2; off <= 2; off++ {
			c := bin + off
			if c >= 0 && c < len(target) {
				smoothed += weights[off+2] * target[c]
				weight += weights[off+2]
			}
		}
		smoothed /= weight
		rate = response.spectralRelease
		if smoothed > r.gain[bin] {
			rate = response.spectralAttack
		}
		r.gain[bin] += rate * (smoothed - r.gain[bin])
		applied := (1 - r.wet*(1-r.gain[bin])) * (1 - r.wet*(1-r.broadGain))
		bins[bin] *= complex(applied, 0)
		if bin > 0 && bin < noiseFFTSize/2 {
			bins[noiseFFTSize-bin] *= complex(applied, 0)
		}
		attenuation += 1 - applied
	}
	r.frames++
	r.minimumAge++
	if r.minimumAge >= 94 {
		r.previousMin = r.minimum
		r.minimum = r.smoothPower
		r.minimumAge = 0
	}
	fft(bins[:], true)
	for i := 0; i < noiseHopSize; i++ {
		r.enqueue(float32(real(bins[i])*r.window[i] + r.overlap[i]))
		r.overlap[i] = real(bins[i+noiseHopSize]) * r.window[i+noiseHopSize]
	}
	r.activity += .18 * (float32(attenuation/float64(len(power))) - r.activity)
	reduction := float32(-20 * math.Log10(math.Max(1e-4, 1-float64(r.activity))))
	r.reductionDB += .12 * (reduction - r.reductionDB)
	r.signal += .12 * (float32(r.voice) - r.signal)
}

func (r *noiseReducer) enqueue(v float32) {
	if r.outputCount == len(r.output) {
		r.outputRead = (r.outputRead + 1) % len(r.output)
		r.outputCount--
	}
	r.output[r.outputWrite] = v
	r.outputWrite = (r.outputWrite + 1) % len(r.output)
	r.outputCount++
}

func fft(v []complex128, inverse bool) {
	for i, j := 1, 0; i < len(v); i++ {
		bit := len(v) >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			v[i], v[j] = v[j], v[i]
		}
	}
	for length := 2; length <= len(v); length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		if inverse {
			angle = -angle
		}
		step := cmplx.Rect(1, angle)
		for start := 0; start < len(v); start += length {
			factor := complex(1, 0)
			for off := 0; off < length/2; off++ {
				even, odd := v[start+off], v[start+off+length/2]*factor
				v[start+off], v[start+off+length/2] = even+odd, even-odd
				factor *= step
			}
		}
	}
	if inverse {
		scale := complex(float64(len(v)), 0)
		for i := range v {
			v[i] /= scale
		}
	}
}
