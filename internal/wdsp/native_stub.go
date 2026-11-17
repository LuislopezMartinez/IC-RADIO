//go:build !windows

package wdsp

type nativeEngine struct{}

func newNativeEngine() *nativeEngine      { return &nativeEngine{} }
func (e *nativeEngine) configure(Config)  {}
func (e *nativeEngine) process([]float32) {}
func (e *nativeEngine) close()            {}
func (e *nativeEngine) status() Status    { return Status{Error: "WDSP requires Windows"} }
