package audioprocessing

import "math"

const (
	pitchBufferSize = 4096
	pitchWindow     = 1024
	lpcOrder        = 14
	lpcFrameSize    = 1024
)

// pitchShifter separates the vocal excitation from its spectral envelope with
// LPC, shifts only the excitation and then restores the original formants.
// Unvoiced sounds are blended with the direct signal to preserve consonants.
type pitchShifter struct {
	buffer         []float64
	write          int
	phase          float64
	voicing        float64
	levelGain      float64
	clarityLowpass float64

	analysis     [lpcFrameSize]float64
	analysisFill int
	lpc          [lpcOrder + 1]float64
	inverseHist  [lpcOrder + 1]float64
	outputHist   [lpcOrder + 1]float64
	residual     []float32
	direct       []float32
}

func (p *pitchShifter) process(samples []float32, semitones, sampleRate float64, quality VoiceShiftQuality,
	autoGain bool, clarity float64, vad, protectConsonants bool) {
	if len(samples) == 0 || sampleRate <= 0 {
		return
	}
	p.updateAnalysis(samples)
	p.updateLPC(sampleRate, quality)

	if cap(p.residual) < len(samples) {
		p.residual = make([]float32, len(samples))
		p.direct = make([]float32, len(samples))
	}
	p.residual = p.residual[:len(samples)]
	p.direct = p.direct[:len(samples)]
	copy(p.direct, samples)

	for i, sample := range samples {
		x := float64(sample)
		r := x
		for order := 1; order <= lpcOrder; order++ {
			r += p.lpc[order] * p.inverseHist[order]
		}
		for order := lpcOrder; order > 1; order-- {
			p.inverseHist[order] = p.inverseHist[order-1]
		}
		p.inverseHist[1] = x
		p.residual[i] = float32(clamp(r, -1.5, 1.5))
	}

	p.shiftResidual(p.residual, semitones)
	directEnergy, outputEnergy := 0.0, 0.0
	minimumWet := .18
	if quality == VoiceShiftNatural {
		minimumWet = .05
	} else if quality == VoiceShiftIntense {
		minimumWet = .38
	}
	for i, shifted := range p.residual {
		y := float64(shifted)
		for order := 1; order <= lpcOrder; order++ {
			y -= p.lpc[order] * p.outputHist[order]
		}
		if math.IsNaN(y) || math.IsInf(y, 0) || math.Abs(y) > 4 {
			y = float64(shifted)
			clear(p.outputHist[:])
		}
		for order := lpcOrder; order > 1; order-- {
			p.outputHist[order] = p.outputHist[order-1]
		}
		p.outputHist[1] = clamp(y, -2, 2)
		wet := 1.0
		if protectConsonants {
			wet = minimumWet + (1-minimumWet)*p.voicing
		}
		if vad && p.voicing < .12 {
			wet = 0
		}
		clarityAlpha := 1 - math.Exp(-2*math.Pi*1500/sampleRate)
		p.clarityLowpass += clarityAlpha * (y - p.clarityLowpass)
		if math.IsNaN(p.clarityLowpass) || math.IsInf(p.clarityLowpass, 0) {
			p.clarityLowpass = y
		}
		y += (y - p.clarityLowpass) * clamp(clarity, 0, 100) / 100 * .35
		mixed := float64(p.direct[i])*(1-wet) + y*wet
		if math.IsNaN(mixed) || math.IsInf(mixed, 0) {
			mixed = float64(p.direct[i])
		}
		directEnergy += float64(p.direct[i]) * float64(p.direct[i])
		outputEnergy += mixed * mixed
		samples[i] = float32(mixed)
	}
	if p.levelGain == 0 {
		p.levelGain = 1
	}
	targetGain := 1.0
	if autoGain && directEnergy > 1e-8 && outputEnergy > 1e-8 {
		targetGain = clamp(math.Sqrt(directEnergy/outputEnergy), .5, 2)
	}
	coefficient := .025
	if targetGain < p.levelGain {
		coefficient = .08
	}
	p.levelGain += coefficient * (targetGain - p.levelGain)
	for i := range samples {
		samples[i] = float32(clamp(float64(samples[i])*p.levelGain, -1, 1))
	}
}

func (p *pitchShifter) updateAnalysis(samples []float32) {
	if len(samples) >= lpcFrameSize {
		start := len(samples) - lpcFrameSize
		for i := range p.analysis {
			p.analysis[i] = float64(samples[start+i])
		}
		p.analysisFill = lpcFrameSize
		return
	}
	copy(p.analysis[:], p.analysis[len(samples):])
	start := lpcFrameSize - len(samples)
	for i, sample := range samples {
		p.analysis[start+i] = float64(sample)
	}
	p.analysisFill += len(samples)
	if p.analysisFill > lpcFrameSize {
		p.analysisFill = lpcFrameSize
	}
}

func (p *pitchShifter) updateLPC(sampleRate float64, quality VoiceShiftQuality) {
	if p.analysisFill < lpcFrameSize/2 {
		return
	}
	mean := 0.0
	for _, sample := range p.analysis {
		mean += sample
	}
	mean /= lpcFrameSize

	var windowed [lpcFrameSize]float64
	for i, sample := range p.analysis {
		window := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(lpcFrameSize-1))
		windowed[i] = (sample - mean) * window
	}
	var autocorrelation [lpcOrder + 1]float64
	for lag := 0; lag <= lpcOrder; lag++ {
		for i := lag; i < lpcFrameSize; i++ {
			autocorrelation[lag] += windowed[i] * windowed[i-lag]
		}
	}
	if autocorrelation[0] < 1e-9 {
		p.voicing *= .85
		return
	}
	coefficients := levinsonDurbin(autocorrelation)
	p.lpc = coefficients

	minLag := int(sampleRate / 400)
	maxLag := int(sampleRate / 75)
	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= lpcFrameSize/2 {
		maxLag = lpcFrameSize/2 - 1
	}
	periodicity := 0.0
	for lag := minLag; lag <= maxLag; lag += 2 {
		cross, energyA, energyB := 0.0, 0.0, 0.0
		for i := maxLag; i < lpcFrameSize; i += 2 {
			a, b := windowed[i], windowed[i-lag]
			cross += a * b
			energyA += a * a
			energyB += b * b
		}
		if denominator := math.Sqrt(energyA * energyB); denominator > 1e-12 {
			periodicity = math.Max(periodicity, cross/denominator)
		}
	}
	threshold, span := .20, .45
	if quality == VoiceShiftNatural {
		threshold, span = .28, .42
	} else if quality == VoiceShiftIntense {
		threshold, span = .12, .38
	}
	target := clamp((periodicity-threshold)/span, 0, 1)
	p.voicing = p.voicing*.88 + target*.12
}

func levinsonDurbin(r [lpcOrder + 1]float64) [lpcOrder + 1]float64 {
	var coefficients [lpcOrder + 1]float64
	coefficients[0] = 1
	errorPower := r[0]
	for order := 1; order <= lpcOrder; order++ {
		reflection := r[order]
		for j := 1; j < order; j++ {
			reflection += coefficients[j] * r[order-j]
		}
		reflection = -reflection / math.Max(errorPower, 1e-12)
		reflection = clamp(reflection, -.98, .98)
		previous := coefficients
		coefficients[order] = reflection
		for j := 1; j < order; j++ {
			coefficients[j] = previous[j] + reflection*previous[order-j]
		}
		errorPower *= 1 - reflection*reflection
		if errorPower < r[0]*1e-6 {
			break
		}
	}
	return coefficients
}

func (p *pitchShifter) shiftResidual(samples []float32, semitones float64) {
	if len(p.buffer) != pitchBufferSize {
		p.buffer = make([]float64, pitchBufferSize)
	}
	ratio := math.Pow(2, semitones/12)
	phaseIncrement := (1 - ratio) / pitchWindow
	for i, sample := range samples {
		p.buffer[p.write] = float64(sample)
		p.phase += phaseIncrement
		p.phase -= math.Floor(p.phase)
		phaseA := p.phase
		phaseB := math.Mod(phaseA+.5, 1)
		delayA := 48 + phaseA*pitchWindow
		delayB := 48 + phaseB*pitchWindow
		weightA := .5 - .5*math.Cos(2*math.Pi*phaseA)
		weightB := .5 - .5*math.Cos(2*math.Pi*phaseB)
		weightSum := weightA + weightB
		if weightSum < 1e-9 {
			weightSum = 1
		}
		shifted := (p.read(float64(p.write)-delayA)*weightA + p.read(float64(p.write)-delayB)*weightB) / weightSum
		samples[i] = float32(clamp(shifted, -1.5, 1.5))
		p.write = (p.write + 1) % pitchBufferSize
	}
}

func (p *pitchShifter) read(position float64) float64 {
	for position < 0 {
		position += pitchBufferSize
	}
	position = math.Mod(position, pitchBufferSize)
	left := int(position)
	right := (left + 1) % pitchBufferSize
	fraction := position - float64(left)
	return p.buffer[left]*(1-fraction) + p.buffer[right]*fraction
}
