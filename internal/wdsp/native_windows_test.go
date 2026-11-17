//go:build windows

package wdsp

import (
	"math"
	"testing"
	"time"
)

func TestNativeRuntimeProcessesAudio(t *testing.T) {
	engine := newNativeEngine()
	defer engine.close()
	if status := engine.status(); !status.Available {
		t.Skip("portable WDSP runtime is not present: " + status.Error)
	}
	engine.configure(Config{
		Enabled: true, Engine: EngineWDSP, Algorithm: AlgorithmNR2,
		Reduction: 60, SignalProtection: 75, AutoNotch: true, NoiseBlanker: true,
	})
	energy, changed := 0.0, false
	for blockIndex := 0; blockIndex < 16; blockIndex++ {
		samples := make([]float32, wdspBlockSize)
		original := make([]float32, wdspBlockSize)
		for i := range samples {
			sampleIndex := blockIndex*wdspBlockSize + i
			samples[i] = float32(.15 * math.Sin(2*math.Pi*900*float64(sampleIndex)/48000))
		}
		copy(original, samples)
		engine.process(samples)
		for i, sample := range samples {
			if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
				t.Fatalf("invalid output in block %d sample %d: %v", blockIndex, i, sample)
			}
			if blockIndex >= 4 {
				energy += float64(sample * sample)
				if math.Abs(float64(sample-original[i])) > 1e-5 {
					changed = true
				}
			}
		}
		time.Sleep(8 * time.Millisecond)
	}
	if energy < 1e-6 {
		t.Fatalf("WDSP produced no steady-state audio: energy=%g", energy)
	}
	if !changed {
		t.Fatal("WDSP never returned a processed block")
	}
}

func TestNR2DoesNotMixDelayedWetSignalWithLiveDryAudio(t *testing.T) {
	engine := newNativeEngine()
	defer engine.close()
	if status := engine.status(); !status.Available {
		t.Skip("portable WDSP runtime is not present: " + status.Error)
	}
	engine.configure(Config{
		Enabled: true, Engine: EngineWDSP, Algorithm: AlgorithmNR2,
		Reduction: 65, SignalProtection: 30,
	})

	// NR2 needs a full FFT frame before it can return processed samples. The
	// second block must therefore contain only its delayed output (silence while
	// priming). The former dry/wet mix leaked the current 0.1 signal here and
	// produced the audible second voice once the delayed branch arrived.
	first := make([]float32, wdspBlockSize)
	second := make([]float32, wdspBlockSize)
	for i := range first {
		first[i], second[i] = .1, .1
	}
	engine.process(first)
	engine.process(second)
	energy := 0.0
	for _, sample := range second {
		energy += float64(sample * sample)
	}
	if energy > 1e-8 {
		t.Fatalf("NR2 still leaks an unaligned dry branch: energy=%g", energy)
	}
}
