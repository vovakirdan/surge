package prof

import (
	"errors"
	"fmt"
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
	// #nosec G304 -- path is controlled by the caller
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("start cpu profile: %w", err),
				fmt.Errorf("close cpu profile output: %w", closeErr),
			)
		}
		return err
	}
	cpuFile = f
	return nil
}

// StopCPU stops an active CPU profile and closes the underlying file.
func StopCPU() {
	pprof.StopCPUProfile()
	if cpuFile != nil {
		if closeErr := cpuFile.Close(); closeErr != nil {
			// Best-effort cleanup; ignore close errors.
			_ = closeErr
		}
		cpuFile = nil
	}
}

// WriteMem captures a heap profile to the supplied file path.
func WriteMem(path string) error {
	// #nosec G304 -- path is controlled by the caller
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
	// #nosec G304 -- path is controlled by the caller
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := trace.Start(f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("start trace: %w", err),
				fmt.Errorf("close trace output: %w", closeErr),
			)
		}
		return err
	}
	traceFile = f
	return nil
}

// StopTrace ends an active runtime trace and closes the file.
func StopTrace() {
	trace.Stop()
	if traceFile != nil {
		if closeErr := traceFile.Close(); closeErr != nil {
			// Best-effort cleanup; ignore close errors.
			_ = closeErr
		}
		traceFile = nil
	}
}
