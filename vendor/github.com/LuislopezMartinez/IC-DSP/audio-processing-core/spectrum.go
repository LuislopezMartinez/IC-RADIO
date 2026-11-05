package audioprocessing

import (
	"math"
	"math/cmplx"
)

const spectrumSize = 512

type Spectrum struct {
	sampleRate   float64
	ring         [spectrumSize]float32
	window       [spectrumSize]float64
	write, count int
}

func NewSpectrum(sampleRate float64) *Spectrum {
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	s := &Spectrum{sampleRate: sampleRate}
	for i := range s.window {
		s.window[i] = .5 - .5*math.Cos(2*math.Pi*float64(i)/float64(spectrumSize-1))
	}
	return s
}

func (s *Spectrum) Add(samples []float32) {
	for _, v := range samples {
		s.ring[s.write] = v
		s.write = (s.write + 1) % len(s.ring)
		s.count = min(s.count+1, len(s.ring))
	}
}

// Calculate fills destination with dBFS bins from DC to maxHz.
func (s *Spectrum) Calculate(destination []float32, maxHz float64) {
	if maxHz <= 0 || maxHz > s.sampleRate/2 {
		maxHz = s.sampleRate / 2
	}
	if s.count < 128 {
		for i := range destination {
			destination[i] = -80
		}
		return
	}
	var bins [spectrumSize]complex128
	start := s.write - s.count
	if start < 0 {
		start += len(s.ring)
	}
	offset := spectrumSize - s.count
	for n := 0; n < s.count; n++ {
		index := (start + n) % len(s.ring)
		windowIndex := offset + n
		bins[windowIndex] = complex(float64(s.ring[index])*s.window[windowIndex], 0)
	}
	fft(bins[:], false)
	var levels [spectrumSize/2 + 1]float64
	for bin := range levels {
		magnitude := 2 * cmplx.Abs(bins[bin]) / float64(spectrumSize)
		levels[bin] = math.Max(20*math.Log10(magnitude+1e-7), -80)
	}
	for bin := range destination {
		frequency := maxHz * float64(bin) / float64(max(len(destination)-1, 1))
		position := frequency * spectrumSize / s.sampleRate
		lower := min(int(position), len(levels)-1)
		upper := min(lower+1, len(levels)-1)
		fraction := position - float64(lower)
		destination[bin] = float32(levels[lower] + fraction*(levels[upper]-levels[lower]))
	}
}

// DetectTone returns the strongest narrow peak suitable for an automatic notch.
func DetectTone(db []float32, maxHz, lowHz, highHz float64) (frequencyHz float64, ok bool) {
	if len(db) < 5 || maxHz <= 0 {
		return 0, false
	}
	first := max(2, int(math.Ceil(lowHz*float64(len(db)-1)/maxHz)))
	last := min(len(db)-3, int(math.Floor(highHz*float64(len(db)-1)/maxHz)))
	best, score := -1, float32(6)
	for i := first; i <= last; i++ {
		s := db[i] - (db[i-2]+db[i+2])*.5
		if db[i] > -55 && s > score {
			best, score = i, s
		}
	}
	if best < 0 {
		return 0, false
	}
	return maxHz * float64(best) / float64(len(db)-1), true
}
