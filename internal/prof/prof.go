package prof

import (
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

var (
	cpuFile   *os.File
	traceFile *os.File
)

// StartCPU enables CPU profiling and writes samples to the provided path.
func StartCPU(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return err
	}
	cpuFile = f
	return nil
}

// StopCPU stops an active CPU profile and closes the underlying file.
func StopCPU() {
	pprof.StopCPUProfile()
	if cpuFile != nil {
		_ = cpuFile.Close()
		cpuFile = nil
	}
}

// WriteMem captures a heap profile to the supplied file path.
func WriteMem(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			panic(closeErr)
		}
	}()
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return err
	}
	return nil
}

// StartTrace writes runtime trace data to the provided path.
func StartTrace(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := trace.Start(f); err != nil {
		_ = f.Close()
		return err
	}
	traceFile = f
	return nil
}

// StopTrace ends an active runtime trace and closes the file.
func StopTrace() {
	trace.Stop()
	if traceFile != nil {
		_ = traceFile.Close()
		traceFile = nil
	}
}
