//go:build windows

package wdsp

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wdspBlockSize = 256
	wdspVersion   = "2.11"
)

type nativeEngine struct {
	mu        sync.Mutex
	dll       *syscall.LazyDLL
	fftw      *syscall.LazyDLL
	procs     map[string]*syscall.LazyProc
	available bool
	opened    bool
	errText   string
	input     [wdspBlockSize]float32
	queued    [wdspBlockSize * 4]float32
	inCount   int
	read      int
	write     int
	count     int
	config    Config
}

var requiredProcedures = []string{
	"ic_wdsp_audio_open",
	"ic_wdsp_audio_close",
	"ic_wdsp_audio_process",
	"ic_wdsp_audio_reset",
}

func newNativeEngine() *nativeEngine {
	e := &nativeEngine{procs: make(map[string]*syscall.LazyProc)}
	path := findRuntimeDLL()
	if path == "" {
		e.errText = "wdsp.dll not found"
		return e
	}
	fftwPath := filepath.Join(filepath.Dir(path), "libfftw3-3.dll")
	e.fftw = syscall.NewLazyDLL(fftwPath)
	if err := e.fftw.Load(); err != nil {
		e.errText = "failed to load libfftw3-3.dll: " + err.Error()
		return e
	}
	e.dll = syscall.NewLazyDLL(path)
	if err := e.dll.Load(); err != nil {
		e.errText = err.Error()
		return e
	}
	for _, name := range requiredProcedures {
		proc := e.dll.NewProc(name)
		if err := proc.Find(); err != nil {
			e.errText = "wdsp.dll is missing " + name
			return e
		}
		e.procs[name] = proc
	}
	e.open()
	return e
}

func findRuntimeDLL() string {
	var candidates []string
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, "wdsp.dll"), filepath.Join(dir, "runtime", "wdsp.dll"))
	}
	candidates = append(candidates, "wdsp.dll", filepath.Join("runtime", "wdsp.dll"))
	if workingDir, err := os.Getwd(); err == nil {
		current := workingDir
		for i := 0; i < 4; i++ {
			candidates = append(candidates, filepath.Join(current, "runtime", "wdsp.dll"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	for _, candidate := range candidates {
		if absolute, err := filepath.Abs(candidate); err == nil {
			if info, statErr := os.Stat(absolute); statErr == nil && !info.IsDir() {
				return absolute
			}
		}
	}
	return ""
}

func (e *nativeEngine) open() {
	result, _, callErr := e.procs["ic_wdsp_audio_open"].Call()
	if result == 0 {
		e.errText = "WDSP audio initialization failed"
		if callErr != syscall.Errno(0) {
			e.errText += ": " + callErr.Error()
		}
		return
	}
	e.opened = true
	e.available = true
}

func (e *nativeEngine) configure(config Config) {
	e.mu.Lock()
	next := config.Normalized()
	reset := e.config.Enabled != next.Enabled || e.config.Engine != next.Engine || e.config.Algorithm != next.Algorithm
	e.config = next
	if reset && e.available {
		e.procs["ic_wdsp_audio_reset"].Call()
		e.input = [wdspBlockSize]float32{}
		e.inCount, e.read, e.write, e.count = 0, 0, 0, 0
	}
	e.mu.Unlock()
}

func (e *nativeEngine) process(samples []float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.available || !e.config.Enabled || e.config.Engine != EngineWDSP {
		return
	}
	for i, sample := range samples {
		e.input[e.inCount] = sample
		e.inCount++
		if e.inCount == wdspBlockSize {
			e.exchange()
			e.inCount = 0
		}
		if e.count > 0 {
			samples[i] = e.queued[e.read]
			e.read = (e.read + 1) % len(e.queued)
			e.count--
		} else {
			// Preserve continuity while the first native block is filling.
			samples[i] = sample
		}
	}
}

func (e *nativeEngine) exchange() {
	dry := e.input
	algorithm := uintptr(0)
	if e.config.Algorithm == AlgorithmNNR {
		algorithm = 1
	}
	model := uintptr(0)
	if e.config.Model == ModelPremium {
		model = 1
	}
	result, _, _ := e.procs["ic_wdsp_audio_process"].Call(
		uintptr(unsafe.Pointer(&e.input[0])),
		wdspBlockSize,
		algorithm,
		model,
		boolArg(e.config.AutoNotch),
		boolArg(e.config.NoiseBlanker),
		uintptr(math.Round(e.config.Reduction)),
		uintptr(math.Round(e.config.SignalProtection)),
	)
	if result == 0 {
		e.errText = "WDSP audio processing failed"
		e.available = false
		e.input = dry
	}
	for i, value := range e.input {
		if !isFinite(value) {
			value = dry[i]
		}
		e.enqueue(value)
	}
}

func (e *nativeEngine) enqueue(value float32) {
	if e.count == len(e.queued) {
		e.read = (e.read + 1) % len(e.queued)
		e.count--
	}
	e.queued[e.write] = value
	e.write = (e.write + 1) % len(e.queued)
	e.count++
}

func isFinite(value float32) bool {
	return !float32IsNaN(value) && value != float32(math.Inf(1)) && value != float32(math.Inf(-1))
}

func float32IsNaN(value float32) bool { return value != value }

func (e *nativeEngine) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opened {
		e.procs["ic_wdsp_audio_close"].Call()
		e.opened = false
	}
	e.available = false
}

func (e *nativeEngine) status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Status{Available: e.available, Version: wdspVersion, Error: e.errText}
}

func boolArg(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}
