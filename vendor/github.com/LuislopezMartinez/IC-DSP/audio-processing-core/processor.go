package audioprocessing

import (
	"math"
	"sync"
)

// Processor is safe to configure, process and inspect from different goroutines.
// Process modifies mono float32 samples in place.
type Processor struct {
	mu                                                      sync.Mutex
	sampleRate                                              float64
	config                                                  Config
	eq                                                      [EQBandCount]biquad
	lowpass                                                 biquad
	notch                                                   notchFilter
	emphasis                                                emphasisFilter
	noise                                                   noiseReducer
	previousInput, highpass, limiterGain                    float64
	compressorEnvelope, compressorGain, compressorGainDB    float64
	blankerPrevious, blankerPrevious2, blankerErrorEnvelope float64
	blankerRemaining                                        int
	ssbClarityLowpass, ssbClarityCurrent                    float64
	txPresenceLowpass, txPresenceCurrent                    float64
	echoBuffer                                              []float64
	echoWrite                                               int
	echoToneLowpass, echoPresenceLowpass                    float64
	beforeNotch, output                                     *Spectrum
	preScratch, dryScratch                                  []float32
}

func NewProcessor(sampleRate float64) *Processor {
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	p := &Processor{sampleRate: sampleRate, config: DefaultConfig(), limiterGain: 1, compressorGain: 1, beforeNotch: NewSpectrum(sampleRate), output: NewSpectrum(sampleRate), echoBuffer: make([]float64, int(sampleRate))}
	p.noise = newNoiseReducer(sampleRate)
	p.Configure(p.config)
	return p
}

func (p *Processor) Configure(c Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c.LowCutHz = clamp(c.LowCutHz, 20, 4000)
	c.HighCutHz = clamp(c.HighCutHz, 250, p.sampleRate*.45)
	if c.NoiseSpeed != NoiseSlow && c.NoiseSpeed != NoiseFast {
		c.NoiseSpeed = NoiseNormal
	}
	c.NoiseStrength = clamp(c.NoiseStrength, 0, 100)
	c.OutputGainDB = clamp(c.OutputGainDB, -60, 12)
	c.ProcessedMix = clamp(c.ProcessedMix, 0, 100)
	c.SSBClarityDB = clamp(c.SSBClarityDB, -6, 9)
	c.TXPresenceDB = clamp(c.TXPresenceDB, 0, 9)
	if c.Compressor.TargetDBFS == 0 {
		c.Compressor.TargetDBFS = -18
	}
	c.Compressor.TargetDBFS = clamp(c.Compressor.TargetDBFS, -30, -6)
	if c.EmphasisMicroseconds != 75 {
		c.EmphasisMicroseconds = 50
	}
	c.Echo.DelayMS = clamp(c.Echo.DelayMS, 40, 500)
	c.Echo.Feedback = clamp(c.Echo.Feedback, 0, 70)
	c.Echo.MixPercent = clamp(c.Echo.MixPercent, 0, 50)
	if c.Echo.Tone != EchoDark && c.Echo.Tone != EchoBright {
		c.Echo.Tone = EchoNeutral
	}
	if c.Echo.Enabled != p.config.Echo.Enabled {
		clear(p.echoBuffer)
		p.echoWrite, p.echoToneLowpass, p.echoPresenceLowpass = 0, 0, 0
	}
	p.config = c
	for i := range p.eq {
		p.eq[i].peak(p.sampleRate, math.Min(EQFrequencies[i], p.sampleRate*.45), c.EQGainsDB[i])
	}
	p.lowpass.lowpass(p.sampleRate, c.HighCutHz)
	p.notch.configure(p.sampleRate, c.Notch)
}

func (p *Processor) Process(samples []float32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hpR := math.Exp(-2 * math.Pi * p.config.LowCutHz / p.sampleRate)
	if cap(p.preScratch) < len(samples) {
		p.preScratch = make([]float32, len(samples))
	}
	if cap(p.dryScratch) < len(samples) {
		p.dryScratch = make([]float32, len(samples))
	}
	pre := p.preScratch[:len(samples)]
	dry := p.dryScratch[:len(samples)]
	copy(dry, samples)
	for i, input32 := range samples {
		input := float64(input32)
		if p.config.NoiseBlankerEnabled {
			prediction := 2*p.blankerPrevious - p.blankerPrevious2
			errorValue := math.Abs(input - prediction)
			threshold := math.Max(.025, p.blankerErrorEnvelope*12)
			if p.blankerRemaining == 0 && errorValue > threshold && math.Abs(input) > .035 {
				p.blankerRemaining = 8
			}
			if p.blankerRemaining > 0 {
				input = clamp(prediction, -1, 1)
				p.blankerRemaining--
			} else {
				p.blankerErrorEnvelope += .002 * (errorValue - p.blankerErrorEnvelope)
			}
			p.blankerPrevious2, p.blankerPrevious = p.blankerPrevious, input
		} else {
			p.blankerPrevious2, p.blankerPrevious = p.blankerPrevious, input
			p.blankerErrorEnvelope, p.blankerRemaining = 0, 0
		}
		p.highpass = input - p.previousInput + hpR*p.highpass
		p.previousInput = input
		filtered := p.lowpass.process(p.highpass)
		pre[i] = float32(filtered)
		samples[i] = float32(p.notch.process(filtered))
	}
	p.beforeNotch.Add(pre)
	if p.config.NoiseEnabled {
		p.noise.process(samples, true, p.config.NoiseStrength, p.config.NoiseSpeed)
	}
	outputGain := math.Pow(10, p.config.OutputGainDB/20)
	mix := p.config.ProcessedMix / 100
	dryGain, processedGain := math.Cos(mix*math.Pi*.5), math.Sin(mix*math.Pi*.5)
	detectorAttack := 1 - math.Exp(-1/(.010*p.sampleRate))
	detectorRelease := 1 - math.Exp(-1/(.250*p.sampleRate))
	gainDown := 1 - math.Exp(-1/(.012*p.sampleRate))
	gainUp := 1 - math.Exp(-1/(.400*p.sampleRate))
	for i, v32 := range samples {
		v := float64(v32)
		if p.config.EQEnabled {
			for band := range p.eq {
				v = p.eq[band].process(v)
			}
		}
		if p.config.Emphasis == EmphasisOff {
			p.ssbClarityCurrent += .002 * (p.config.SSBClarityDB - p.ssbClarityCurrent)
			clarityAlpha := 1 - math.Exp(-2*math.Pi*900/p.sampleRate)
			p.ssbClarityLowpass += clarityAlpha * (v - p.ssbClarityLowpass)
			lowGain := math.Pow(10, -p.ssbClarityCurrent/80)
			highGain := math.Pow(10, p.ssbClarityCurrent/20)
			v = p.ssbClarityLowpass*lowGain + (v-p.ssbClarityLowpass)*highGain
		}
		v = p.emphasis.process(v, p.sampleRate, p.config.Emphasis, p.config.EmphasisMicroseconds)
		p.txPresenceCurrent += .002 * (p.config.TXPresenceDB - p.txPresenceCurrent)
		presenceAlpha := 1 - math.Exp(-2*math.Pi*1800/p.sampleRate)
		p.txPresenceLowpass += presenceAlpha * (v - p.txPresenceLowpass)
		presenceGain := math.Pow(10, p.txPresenceCurrent/20)
		v = p.txPresenceLowpass + (v-p.txPresenceLowpass)*presenceGain
		if p.config.Compressor.Enabled {
			power := v * v
			detectorRate := detectorRelease
			if power > p.compressorEnvelope {
				detectorRate = detectorAttack
			}
			p.compressorEnvelope += detectorRate * (power - p.compressorEnvelope)
			levelDB := 10 * math.Log10(math.Max(p.compressorEnvelope, 1e-12))
			desiredGainDB := p.compressorGainDB
			if levelDB > -60 {
				desiredGainDB = clamp(p.config.Compressor.TargetDBFS-levelDB, -24, 18)
			}
			gainRate := gainUp
			if desiredGainDB < p.compressorGainDB {
				gainRate = gainDown
			}
			p.compressorGainDB += gainRate * (desiredGainDB - p.compressorGainDB)
			p.compressorGain = math.Pow(10, p.compressorGainDB/20)
			v *= p.compressorGain
		} else {
			p.compressorEnvelope, p.compressorGain, p.compressorGainDB = 0, 1, 0
		}
		if p.config.Echo.Enabled && len(p.echoBuffer) > 0 {
			delaySamples := max(1, min(len(p.echoBuffer)-1, int(p.config.Echo.DelayMS*p.sampleRate/1000)))
			read := p.echoWrite - delaySamples
			if read < 0 {
				read += len(p.echoBuffer)
			}
			rawWet := p.echoBuffer[read]
			cutoff := 2500.0
			if p.config.Echo.Tone == EchoDark {
				cutoff = 1100
			} else if p.config.Echo.Tone == EchoBright {
				cutoff = 3900
			}
			toneAlpha := 1 - math.Exp(-2*math.Pi*cutoff/p.sampleRate)
			p.echoToneLowpass += toneAlpha * (rawWet - p.echoToneLowpass)
			wet := p.echoToneLowpass
			if p.config.Echo.Tone == EchoBright {
				// Lift the intelligibility band of the repeats without extending it
				// beyond a typical radio passband. The added band is approximately
				// 1.8–3.9 kHz, so Bright remains audible on SSB/FM equipment.
				presenceAlpha := 1 - math.Exp(-2*math.Pi*1800/p.sampleRate)
				p.echoPresenceLowpass += presenceAlpha * (rawWet - p.echoPresenceLowpass)
				wet += .35 * (wet - p.echoPresenceLowpass)
			} else {
				p.echoPresenceLowpass = wet
			}
			feedback := p.config.Echo.Feedback / 100
			p.echoBuffer[p.echoWrite] = clamp(v+wet*feedback, -1.5, 1.5)
			p.echoWrite = (p.echoWrite + 1) % len(p.echoBuffer)
			echoMix := p.config.Echo.MixPercent / 100
			v = v*(1-echoMix) + wet*echoMix
		}
		v = (float64(dry[i])*dryGain + v*processedGain) * outputGain
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		desired := 1.0
		if math.Abs(v) > .98 {
			desired = .98 / math.Abs(v)
		}
		if desired < p.limiterGain {
			p.limiterGain = desired
		} else {
			p.limiterGain += (1 - math.Exp(-1/(.1*p.sampleRate))) * (desired - p.limiterGain)
		}
		samples[i] = float32(clamp(v*p.limiterGain, -.98, .98))
	}
	p.output.Add(samples)
}

func (p *Processor) Spectrum(destination []float32, maxHz float64) {
	p.mu.Lock()
	snapshot := *p.output
	p.mu.Unlock()
	snapshot.Calculate(destination, maxHz)
}
func (p *Processor) SpectrumBeforeNotch(destination []float32, maxHz float64) {
	p.mu.Lock()
	snapshot := *p.beforeNotch
	p.mu.Unlock()
	snapshot.Calculate(destination, maxHz)
}
func (p *Processor) NoiseStatus() NoiseStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.noise.status()
}
func (p *Processor) CompressorStatus() CompressorStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return CompressorStatus{GainDB: float32(p.compressorGainDB)}
}
func (p *Processor) ResetNoiseLearning() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.noise = newNoiseReducer(p.sampleRate)
}
