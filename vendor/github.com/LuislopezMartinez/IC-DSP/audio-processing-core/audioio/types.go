package audioio

// Device is an audio endpoint exposed to the UI.
type Device struct {
	Name      string
	IsDefault bool
	index     int
}

// Status is a lock-free-friendly snapshot of the stream state.
type Status struct {
	Running       bool
	InputDBFS     float32
	OutputDBFS    float32
	InputRMSDBFS  float32
	OutputRMSDBFS float32
	CPULoad       float32
	SampleRate    int
	BufferFrames  int
	Error         string
}

// Processor is implemented by audioprocessing.Processor.
type Processor interface {
	Process([]float32)
}
