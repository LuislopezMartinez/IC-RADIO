package audioprocessing

import "math"

// pitchShifter uses two overlapping variable-delay grains. Their Hann windows
// crossfade at the delay discontinuities, changing pitch while preserving the
// duration of the stream. A gentle spectral tilt keeps modest voice shifts
// from becoming unnaturally thin or boomy.
type pitchShifter struct {
	buffer  []float64
	write   int
	phase   float64
	toneLow float64
}

func (p *pitchShifter) process(samples []float32, semitones, sampleRate float64) {
	const window = 1024
	const baseDelay = 48
	if len(p.buffer) != 4096 {
		p.buffer = make([]float64, 4096)
		p.write, p.phase, p.toneLow = 0, 0, 0
	}
	ratio := math.Pow(2, semitones/12)
	phaseStep := math.Abs(ratio-1) / window
	toneAlpha := 1 - math.Exp(-2*math.Pi*900/sampleRate)
	compensation := math.Abs(semitones) / 4
	lowGain, highGain := 1.0, 1.0
	if semitones > 0 {
		lowGain, highGain = 1+.18*compensation, 1-.10*compensation
	} else {
		lowGain, highGain = 1-.08*compensation, 1+.12*compensation
	}
	for i, sample := range samples {
		p.buffer[p.write] = float64(sample)
		phaseA := p.phase
		phaseB := math.Mod(phaseA+.5, 1)
		delay := func(phase float64) float64 {
			if ratio >= 1 {
				return baseDelay + (1-phase)*window
			}
			return baseDelay + phase*window
		}
		weightA := .5 - .5*math.Cos(2*math.Pi*phaseA)
		weightB := .5 - .5*math.Cos(2*math.Pi*phaseB)
		shifted := p.read(delay(phaseA))*weightA + p.read(delay(phaseB))*weightB
		p.toneLow += toneAlpha * (shifted - p.toneLow)
		shifted = p.toneLow*lowGain + (shifted-p.toneLow)*highGain
		samples[i] = float32(clamp(shifted, -1, 1))
		p.write = (p.write + 1) % len(p.buffer)
		p.phase += phaseStep
		if p.phase >= 1 {
			p.phase -= 1
		}
	}
}

func (p *pitchShifter) read(delay float64) float64 {
	position := float64(p.write) - delay
	for position < 0 {
		position += float64(len(p.buffer))
	}
	index := int(position) % len(p.buffer)
	next := (index + 1) % len(p.buffer)
	fraction := position - math.Floor(position)
	return p.buffer[index]*(1-fraction) + p.buffer[next]*fraction
}
