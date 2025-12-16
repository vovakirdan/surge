package vm

import (
	"io"
	"os"
	"strconv"
	"strings"
)

// Runtime provides the interface between the VM and the outside world.
type Runtime interface {
	// Argv returns command-line arguments (excluding program name).
	Argv() []string

	// StdinReadAll reads all content from stdin as a string.
	StdinReadAll() string

	// Exit signals the VM to halt with the given exit code.
	Exit(code int)

	// ParseArgInt parses a string as an integer.
	ParseArgInt(s string) (int, error)

	// ExitCode returns the exit code set by Exit, or -1 if not set.
	ExitCode() int

	// Exited returns true if Exit was called.
	Exited() bool
}

// DefaultRuntime implements Runtime using OS facilities.
type DefaultRuntime struct {
	exitCode int
	exited   bool
}

// NewDefaultRuntime creates a runtime using OS facilities.
func NewDefaultRuntime() *DefaultRuntime {
	return &DefaultRuntime{exitCode: -1}
}

func (r *DefaultRuntime) Argv() []string {
	if len(os.Args) > 1 {
		return os.Args[1:]
	}
	return nil
}

func (r *DefaultRuntime) StdinReadAll() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *DefaultRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

func (r *DefaultRuntime) ParseArgInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	return strconv.Atoi(s)
}

func (r *DefaultRuntime) ExitCode() int {
	return r.exitCode
}

func (r *DefaultRuntime) Exited() bool {
	return r.exited
}

// TestRuntime implements Runtime with controlled inputs for testing.
type TestRuntime struct {
	argv     []string
	stdin    string
	exitCode int
	exited   bool
}

// NewTestRuntime creates a test runtime with controlled inputs.
func NewTestRuntime(argv []string, stdin string) *TestRuntime {
	return &TestRuntime{
		argv:     argv,
		stdin:    stdin,
		exitCode: -1,
	}
}

func (r *TestRuntime) Argv() []string {
	return r.argv
}

func (r *TestRuntime) StdinReadAll() string {
	return strings.TrimSpace(r.stdin)
}

func (r *TestRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

func (r *TestRuntime) ParseArgInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	return strconv.Atoi(s)
}

func (r *TestRuntime) ExitCode() int {
	return r.exitCode
}

func (r *TestRuntime) Exited() bool {
	return r.exited
}
