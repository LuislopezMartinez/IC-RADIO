package wdsp

import (
	"strings"
	"sync/atomic"
)

const (
	EngineClassic = "classic"
	EngineWDSP    = "wdsp"
	AlgorithmNR2  = "nr2"
	AlgorithmNNR  = "nnr"
	ModelStandard = "standard"
	ModelPremium  = "premium"
)

// Config is persisted with the RX profile. WDSP is deliberately RX-only:
// IC-RADIO receives already-demodulated mono audio from the transceiver.
type Config struct {
	Enabled          bool    `json:"enabled"`
	Engine           string  `json:"engine"`
	Algorithm        string  `json:"algorithm"`
	Model            string  `json:"model"`
	Reduction        float64 `json:"reduction"`
	SignalProtection float64 `json:"signal_protection"`
	AutoNotch        bool    `json:"auto_notch"`
	NoiseBlanker     bool    `json:"noise_blanker"`
}

func DefaultConfig() Config {
	return Config{
		Enabled: true, Engine: EngineClassic, Algorithm: AlgorithmNNR,
		Model: ModelStandard, Reduction: 60, SignalProtection: 75,
		AutoNotch: true, NoiseBlanker: true,
	}
}

func (c Config) Normalized() Config {
	c.Engine = strings.ToLower(c.Engine)
	if c.Engine != EngineWDSP {
		c.Engine = EngineClassic
	}
	c.Algorithm = strings.ToLower(c.Algorithm)
	if c.Algorithm != AlgorithmNR2 {
		c.Algorithm = AlgorithmNNR
	}
	c.Model = strings.ToLower(c.Model)
	if c.Model != ModelPremium {
		c.Model = ModelStandard
	}
	c.Reduction = clamp(c.Reduction, 0, 100)
	c.SignalProtection = clamp(c.SignalProtection, 0, 100)
	return c
}

func clamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

type Status struct {
	Available bool
	Active    bool
	Version   string
	Error     string
}

type Processor interface {
	Process([]float32)
}

// Chain inserts WDSP before IC-DSP. If the native runtime is absent or fails,
// samples continue through the classic processor without interruption.
type Chain struct {
	downstream Processor
	native     *nativeEngine
	config     atomic.Value
}

func NewChain(downstream Processor) *Chain {
	chain := &Chain{downstream: downstream, native: newNativeEngine()}
	chain.config.Store(DefaultConfig())
	return chain
}

func (c *Chain) Configure(config Config) {
	config = config.Normalized()
	c.config.Store(config)
	if c.native != nil {
		c.native.configure(config)
	}
}

func (c *Chain) Process(samples []float32) {
	config := c.config.Load().(Config)
	if config.Enabled && config.Engine == EngineWDSP && c.native != nil {
		c.native.process(samples)
	}
	c.downstream.Process(samples)
}

func (c *Chain) Status() Status {
	if c.native == nil {
		return Status{Error: "WDSP runtime unavailable"}
	}
	status := c.native.status()
	config := c.config.Load().(Config)
	status.Active = status.Available && config.Enabled && config.Engine == EngineWDSP
	return status
}

func (c *Chain) Close() {
	if c.native != nil {
		c.native.close()
	}
}
