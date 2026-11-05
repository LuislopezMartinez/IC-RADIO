//go:build !cgo

package audioio

import "sync"

// Engine keeps the UI usable in builds where CGO/miniaudio is unavailable.
type Engine struct {
	mu     sync.RWMutex
	status Status
}

func New(Processor) (*Engine, error) {
	e := &Engine{}
	e.status = Status{SampleRate: 48000, BufferFrames: 480, InputDBFS: -80, OutputDBFS: -80, InputRMSDBFS: -80, OutputRMSDBFS: -80, Error: "Audio no disponible: compile con CGO_ENABLED=1 y un compilador C"}
	return e, nil
}
func (e *Engine) Close()               {}
func (e *Engine) Inputs() []Device     { return []Device{{Name: "Audio no disponible", IsDefault: true}} }
func (e *Engine) Outputs() []Device    { return []Device{{Name: "Audio no disponible", IsDefault: true}} }
func (e *Engine) Refresh() error       { return nil }
func (e *Engine) Start(int, int) error { return nil }
func (e *Engine) Stop()                {}
func (e *Engine) Status() Status       { e.mu.RLock(); defer e.mu.RUnlock(); return e.status }
