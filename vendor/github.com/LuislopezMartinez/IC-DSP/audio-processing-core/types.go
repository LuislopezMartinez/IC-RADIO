package audioprocessing

const DefaultSampleRate = 48_000

// EmphasisMode selects the first-order FM emphasis network.
type EmphasisMode uint8

const (
	EmphasisOff EmphasisMode = iota
	PreEmphasis
	DeEmphasis
)

// NoiseSpeed controls how quickly the reducer closes after wanted audio ends.
type NoiseSpeed string

const (
	NoiseSlow   NoiseSpeed = "slow"
	NoiseNormal NoiseSpeed = "normal"
	NoiseFast   NoiseSpeed = "fast"
)

type NoiseStatus struct {
	ReductionDB float32
	Signal      float32
	Learning    bool
}

type NotchConfig struct {
	Enabled     bool
	FrequencyHz float64
	WidthHz     float64
	DepthDB     float64
}

type CompressorConfig struct {
	Enabled    bool
	TargetDBFS float64
}

type CompressorStatus struct {
	GainDB float32
}

type EchoTone string

const (
	EchoDark    EchoTone = "dark"
	EchoNeutral EchoTone = "neutral"
	EchoBright  EchoTone = "bright"
)

type EchoConfig struct {
	Enabled    bool
	DelayMS    float64
	Feedback   float64
	MixPercent float64
	Tone       EchoTone
}

type Config struct {
	LowCutHz, HighCutHz  float64
	EQEnabled            bool
	EQGainsDB            [EQBandCount]float64
	Notch                NotchConfig
	NoiseEnabled         bool
	NoiseBlankerEnabled  bool
	NoiseStrength        float64
	NoiseSpeed           NoiseSpeed
	Emphasis             EmphasisMode
	EmphasisMicroseconds int
	SSBClarityDB         float64
	OutputGainDB         float64
	ProcessedMix         float64
	Compressor           CompressorConfig
	Echo                 EchoConfig
}

func DefaultConfig() Config {
	return Config{
		LowCutHz: 100, HighCutHz: 4000, EQEnabled: true,
		Notch:         NotchConfig{FrequencyHz: 1000, WidthHz: 120, DepthDB: -35},
		NoiseStrength: 50, NoiseSpeed: NoiseNormal,
		EmphasisMicroseconds: 50,
		SSBClarityDB:         0,
		OutputGainDB:         0,
		ProcessedMix:         100,
		Compressor: CompressorConfig{
			TargetDBFS: -18,
		},
		Echo: EchoConfig{DelayMS: 120, Feedback: 15, MixPercent: 10, Tone: EchoNeutral},
	}
}

// MigrateLegacyEQ expands the original five stored gains into the current
// nine-band layout. Original bands are preserved and new bands are linearly
// interpolated in dB, which keeps old presets perceptually continuous.
func MigrateLegacyEQ(c *Config) {
	old := c.EQGainsDB
	c.EQGainsDB = [EQBandCount]float64{
		old[0], (old[0] + old[1]) / 2,
		old[1], (old[1] + old[2]) / 2,
		old[2], (old[2] + old[3]) / 2,
		old[3], (old[3] + old[4]) / 2,
		old[4],
	}
}
