package wdsp

import "testing"

func TestConfigNormalization(t *testing.T) {
	config := (Config{Engine: "unknown", Algorithm: "unknown", Model: "unknown", Reduction: 120, SignalProtection: -5}).Normalized()
	if config.Engine != EngineClassic || config.Algorithm != AlgorithmNNR || config.Model != ModelStandard {
		t.Fatalf("unexpected normalized choices: %+v", config)
	}
	if config.Reduction != 100 || config.SignalProtection != 0 {
		t.Fatalf("unexpected normalized levels: %+v", config)
	}
}

type countingProcessor struct{ calls int }

func (p *countingProcessor) Process([]float32) { p.calls++ }

func TestChainAlwaysRunsDownstream(t *testing.T) {
	downstream := &countingProcessor{}
	chain := NewChain(downstream)
	defer chain.Close()
	chain.Configure(Config{Enabled: true, Engine: EngineWDSP, Algorithm: AlgorithmNNR})
	chain.Process(make([]float32, 32))
	if downstream.calls != 1 {
		t.Fatalf("downstream calls = %d, want 1", downstream.calls)
	}
}
