package vm_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestLLVMParity(t *testing.T) {
	skipTimeoutTests(t)
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM parity test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM parity test")
	}

	surge := buildSurgeBinary(t, root)
	parityEnv := envForParity(root)

	cases := []struct {
		name  string
		file  string
		setup func(t *testing.T) []string
	}{
		{name: "async_rt_exit", file: "async_rt_exit.sg"},
		{name: "async_panic", file: "async_panic.sg"},
		{name: "exit_code", file: "exit_code.sg"},
		{name: "panic", file: "panic.sg"},
		{name: "string_concat", file: "string_concat.sg"},
		{name: "from_str_fixed_width", file: "from_str_fixed_width.sg"},
		{name: "array_range_indexing", file: "array_range_indexing.sg"},
		{name: "byte_array_append_string", file: "byte_array_append_string.sg"},
		{name: "stdlib_bytes", file: "stdlib_bytes.sg"},
		{name: "tagged_switch", file: "tagged_switch.sg"},
		{name: "nested_ref_field", file: "nested_ref_field.sg"},
		{name: "option_tag_cast", file: "option_tag_cast.sg"},
		{name: "channel_new_turbofish", file: "channel_new_turbofish.sg"},
		{name: "select_channel", file: "select_channel.sg"},
		{name: "select_timeout", file: "select_timeout.sg"},
		{name: "unicode_len", file: "unicode_len.sg"},
		{name: "entropy_len", file: "entropy_len.sg"},
		{name: "random_pcg32", file: "random_pcg32.sg"},
		{name: "hash_xxh64", file: "hash_xxh64.sg"},
		{name: "hash_stable64", file: "hash_stable64.sg"},
		{name: "uuid_v4", file: "uuid_v4.sg"},
		{name: "path_smoke", file: "path_smoke.sg"},
		{
			name: "fs_dir_smoke",
			file: "fs_dir_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bravo"), 0o600); err != nil {
					t.Fatalf("write b.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "fs_metadata_smoke",
			file: "fs_metadata_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "file_rw_smoke",
			file: "file_rw_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				return []string{dir}
			},
		},
		{
			name: "file_seek_head_tail_smoke_lowlevel",
			file: "file_seek_head_tail_smoke_lowlevel.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				return []string{dir}
			},
		},
		{name: "net_listen_close", file: "net_listen_close.sg"},
		{
			name: "net_listen_reuseaddr",
			file: "net_listen_reuseaddr.sg",
			setup: func(t *testing.T) []string {
				return []string{strconv.Itoa(pickFreePort(t))}
			},
		},
		{name: "net_echo", file: "net_echo.sg"},
		{name: "http_parse_request", file: "http_parse_request.sg"},
		{name: "http_parse_request_strict", file: "http_parse_request_strict.sg"},
		{name: "http_response_bytes", file: "http_response_bytes.sg"},
		{name: "http_response_formats", file: "http_response_formats.sg"},
		{name: "http_context_helpers", file: "http_context_helpers.sg"},
		{name: "http_helpers", file: "http_helpers.sg"},
		{name: "http_cookie_helpers", file: "http_cookie_helpers.sg"},
		{name: "http_query_helpers", file: "http_query_helpers.sg"},
		{name: "http_chunked_request", file: "http_chunked_request.sg"},
		{name: "http_chunked_response", file: "http_chunked_response.sg"},
		{name: "http_server", file: "http_server.sg"},
		{name: "http_connect", file: "http_connect.sg"},
		{
			name: "head_tail_text",
			file: "head_tail_text.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "text.txt"), []byte("hello world"), 0o600); err != nil {
					t.Fatalf("write text.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "walkdir_for_in",
			file: "walkdir_for_in.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(dir, "a"), 0o700); err != nil {
					t.Fatalf("mkdir a: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(dir, "b", "sub"), 0o700); err != nil {
					t.Fatalf("mkdir b/sub: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o600); err != nil {
					t.Fatalf("write root.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "a", "a.txt"), []byte("a"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "b", "sub", "b.txt"), []byte("b"), 0o600); err != nil {
					t.Fatalf("write b.txt: %v", err)
				}
				return []string{dir}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity", tc.file))

			if tc.name == "net_echo" {
				message := "hello"
				vmOut, vmErr, vmCode := runNetEchoSurge(t, root, surge, sgRel, message)

				buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
				if buildCode != 0 {
					t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
				}

				binPath := filepath.Join(root, "target", "debug", tc.name)
				llOut, llErr, llCode := runNetEchoBinary(t, binPath, message)

				if llCode != vmCode {
					t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
				}
				if llOut != vmOut {
					t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
				}
				if llErr != vmErr {
					t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
				}
				return
			}
			if tc.name == "http_server" {
				vmOut, vmErr, vmCode := runHTTPServerSurge(t, root, surge, sgRel)

				buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
				if buildCode != 0 {
					t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
				}

				binPath := filepath.Join(root, "target", "debug", tc.name)
				llOut, llErr, llCode := runHTTPServerBinary(t, binPath)

				if llCode != vmCode {
					t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
				}
				if llOut != vmOut {
					t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
				}
				if llErr != vmErr {
					t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
				}
				return
			}
			if tc.name == "http_connect" {
				vmOut, vmErr, vmCode := runHTTPConnectSurge(t, root, surge, sgRel)

				buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
				if buildCode != 0 {
					t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
				}

				binPath := filepath.Join(root, "target", "debug", tc.name)
				llOut, llErr, llCode := runHTTPConnectBinary(t, binPath)

				if llCode != vmCode {
					t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
				}
				if llOut != vmOut {
					t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
				}
				if llErr != vmErr {
					t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
				}
				return
			}

			var progArgs []string
			if tc.setup != nil {
				progArgs = tc.setup(t)
			}
			args := []string{"run", "--backend=vm", sgRel}
			if len(progArgs) > 0 {
				args = append(args, "--")
				args = append(args, progArgs...)
			}
			vmOut, vmErr, vmCode := runSurgeWithEnv(t, root, surge, parityEnv, args...)

			buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, parityEnv, "build", sgRel)
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "target", "debug", tc.name)
			llOut, llErr, llCode := runBinary(t, binPath, progArgs...)

			if llCode != vmCode {
				t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
			}
			if llOut != vmOut {
				t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
			}
			if llErr != vmErr {
				t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
			}
		})
	}
}

// runBinary is defined in llvm_smoke_test.go

func runNetEchoSurge(t *testing.T, root, surge, sgRel, message string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	cmd := exec.Command(surge, "run", "--backend=vm", sgRel, "--", portStr, message)
	cmd.Dir = root
	cmd.Env = envForParity(root)
	return runNetEchoCommand(t, cmd, port, message)
}

func runNetEchoBinary(t *testing.T, path, message string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	cmd := exec.Command(path, portStr, message)
	cmd.Env = envForParity(repoRoot(t))
	return runNetEchoCommand(t, cmd, port, message)
}

func runNetEchoCommand(t *testing.T, cmd *exec.Cmd, port int, message string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start net echo command: %v", err)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("dial net echo server: %v\nstderr:\n%s", err, errBuf.String())
	}

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(message)); err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("write net echo payload: %v", err)
	}

	buf := make([]byte, len(message))
	if _, err := io.ReadFull(conn, buf); err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("read net echo payload: %v", err)
	}
	_ = conn.Close()
	if string(buf) != message {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("echo mismatch: got %q want %q", string(buf), message)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		stdout = outBuf.String()
		stderr = errBuf.String()
		stdout = stripTimingLines(stdout)
		if err == nil {
			return stdout, stderr, 0
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run command: %v\nstderr:\n%s", err, stderr)
		}
		return stdout, stderr, exitErr.ExitCode()
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-waitCh
		t.Fatalf("net echo command timed out")
		return "", "", 1
	}
}

func runHTTPServerSurge(t *testing.T, root, surge, sgRel string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	cmd := exec.Command(surge, "run", "--backend=vm", "--real-time", sgRel, "--", portStr)
	cmd.Dir = root
	cmd.Env = envForParity(root)
	return runHTTPServerCommand(t, cmd, port)
}

func runHTTPServerBinary(t *testing.T, path string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	cmd := exec.Command(path, portStr)
	cmd.Env = envForParity(repoRoot(t))
	return runHTTPServerCommand(t, cmd, port)
}

func runHTTPConnectSurge(t *testing.T, root, surge, sgRel string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	return runSurgeWithEnv(t, root, surge, envForParity(root), "run", "--backend=vm", sgRel, "--", portStr)
}

func runHTTPConnectBinary(t *testing.T, path string) (stdout, stderr string, exitCode int) {
	t.Helper()
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	return runBinary(t, path, portStr)
}

func runHTTPServerCommand(t *testing.T, cmd *exec.Cmd, port int) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server command: %v", err)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	fail := func(action string, err error) {
		running := false
		if cmd.Process != nil {
			if sigErr := cmd.Process.Signal(syscall.Signal(0)); sigErr == nil {
				running = true
			}
		}
		if os.Getenv("SURGE_TRACE_EXEC") != "" && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGUSR1)
			time.Sleep(50 * time.Millisecond)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		outStr := stripTimingLines(outBuf.String())
		errStr := errBuf.String()
		t.Fatalf("%s: %v (running=%v)\nstdout:\n%s\nstderr:\n%s", action, err, running, outStr, errStr)
	}

	if err := runHTTPKeepaliveScenario(addr); err != nil {
		fail("keepalive scenario failed", err)
	}
	if err := runHTTPPipeliningScenario(addr); err != nil {
		fail("pipelining scenario failed", err)
	}
	if err := runHTTPConcurrentScenario(addr); err != nil {
		fail("concurrent scenario failed", err)
	}
	if err := runHTTPOverflowScenario(addr); err != nil {
		fail("overflow scenario failed", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	_ = cmd.Process.Kill()
	select {
	case <-waitCh:
		stdout = outBuf.String()
		stderr = errBuf.String()
		stdout = stripTimingLines(stdout)
		return stdout, stderr, 0
	case <-time.After(15 * time.Second):
		t.Fatalf("http server command timed out")
		return "", "", 1
	}
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected addr type: %T", ln.Addr())
	}
	return addr.Port
}

func dialWithRetry(addr string, deadline time.Time) (net.Conn, error) {
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("dial timeout")
	}
	return nil, lastErr
}
