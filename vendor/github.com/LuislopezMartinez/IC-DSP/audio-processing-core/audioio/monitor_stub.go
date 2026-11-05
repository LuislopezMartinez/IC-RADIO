//go:build !cgo

package audioio

type PlaybackMonitor struct{}

func NewPlaybackMonitor() (*PlaybackMonitor, error) { return &PlaybackMonitor{}, nil }
func (m *PlaybackMonitor) Start(int) error          { return nil }
func (m *PlaybackMonitor) StartNamed(string) error  { return nil }
func (m *PlaybackMonitor) Write([]float32)          {}
func (m *PlaybackMonitor) Stop()                    {}
func (m *PlaybackMonitor) Close()                   {}
