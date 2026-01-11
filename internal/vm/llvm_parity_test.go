package vm_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLLVMParity(t *testing.T) {
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
		{name: "exit_code", file: "exit_code.sg"},
		{name: "panic", file: "panic.sg"},
		{name: "string_concat", file: "string_concat.sg"},
		{name: "tagged_switch", file: "tagged_switch.sg"},
		{name: "select_channel", file: "select_channel.sg"},
		{name: "select_timeout", file: "select_timeout.sg"},
		{name: "unicode_len", file: "unicode_len.sg"},
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
		{name: "net_echo", file: "net_echo.sg"},
		{name: "http_parse_request", file: "http_parse_request.sg"},
		{name: "http_response_bytes", file: "http_response_bytes.sg"},
		{name: "http_chunked_request", file: "http_chunked_request.sg"},
		{name: "http_chunked_response", file: "http_chunked_response.sg"},
		{name: "http_json_helpers", file: "http_json_helpers.sg"},
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
		t.Fatalf("http server command timed out")
		return "", "", 1
	}
}

func runHTTPKeepaliveScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	reader := bufio.NewReader(conn)

	req1 := "GET /one HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = conn.Write([]byte(req1)); err != nil {
		return fmt.Errorf("write keepalive req1: %w", err)
	}
	status, body, err := readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp1: %w", err)
	}
	if status != 200 || body != "one" {
		return fmt.Errorf("keepalive resp1 mismatch: status=%d body=%q", status, body)
	}

	req2 := "GET /two HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = conn.Write([]byte(req2)); err != nil {
		return fmt.Errorf("write keepalive req2: %w", err)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp2: %w", err)
	}
	if status != 200 || body != "two" {
		return fmt.Errorf("keepalive resp2 mismatch: status=%d body=%q", status, body)
	}
	return nil
}

func runHTTPPipeliningScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	reader := bufio.NewReader(conn)

	req := strings.Join([]string{
		"GET /slow HTTP/1.1\r\nHost: example\r\n\r\n",
		"GET /fast HTTP/1.1\r\nHost: example\r\n\r\n",
	}, "")
	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write pipeline reqs: %w", err)
	}
	status, body, err := readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read pipeline resp1: %w", err)
	}
	if status != 200 || body != "slow" {
		return fmt.Errorf("pipeline resp1 mismatch: status=%d body=%q", status, body)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read pipeline resp2: %w", err)
	}
	if status != 200 || body != "fast" {
		return fmt.Errorf("pipeline resp2 mismatch: status=%d body=%q", status, body)
	}
	return nil
}

func runHTTPConcurrentScenario(addr string) error {
	slowConn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial slow: %w", err)
	}
	defer slowConn.Close()
	if err = slowConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set slow deadline: %w", err)
	}
	slowReader := bufio.NewReader(slowConn)

	slowReq := "GET /slow HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = slowConn.Write([]byte(slowReq)); err != nil {
		return fmt.Errorf("write slow req: %w", err)
	}

	fastConn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial fast: %w", err)
	}
	defer fastConn.Close()
	fastReader := bufio.NewReader(fastConn)

	fastReq := "GET /fast HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = fastConn.Write([]byte(fastReq)); err != nil {
		return fmt.Errorf("write fast req: %w", err)
	}
	if err = fastConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		return fmt.Errorf("set fast deadline: %w", err)
	}
	status, body, err := readHTTPResponse(fastReader)
	if err != nil {
		return fmt.Errorf("read fast resp: %w", err)
	}
	if status != 200 || body != "fast" {
		return fmt.Errorf("fast resp mismatch: status=%d body=%q", status, body)
	}

	if err = slowConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("set slow read deadline: %w", err)
	}
	status, body, err = readHTTPResponse(slowReader)
	if err != nil {
		return fmt.Errorf("read slow resp: %w", err)
	}
	if status != 200 || body != "slow" {
		return fmt.Errorf("slow resp mismatch: status=%d body=%q", status, body)
	}
	return nil
}

func runHTTPOverflowScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	reader := bufio.NewReader(conn)

	req := strings.Join([]string{
		"GET /a HTTP/1.1\r\nHost: example\r\n\r\n",
		"GET /b HTTP/1.1\r\nHost: example\r\n\r\n",
		"GET /c HTTP/1.1\r\nHost: example\r\n\r\n",
	}, "")
	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write overflow reqs: %w", err)
	}
	status, body, err := readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read overflow resp1: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("overflow resp1 mismatch: status=%d body=%q", status, body)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read overflow resp2: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("overflow resp2 mismatch: status=%d body=%q", status, body)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read overflow resp3: %w", err)
	}
	if status != 503 || body != "" {
		return fmt.Errorf("overflow resp3 mismatch: status=%d body=%q", status, body)
	}
	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		return fmt.Errorf("set overflow read deadline: %w", err)
	}
	if _, err := reader.ReadByte(); err == nil || !errors.Is(err, io.EOF) {
		return errors.New("expected connection close after overflow")
	}
	return nil
}

func readHTTPResponse(r *bufio.Reader) (status int, body string, err error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return 0, "", errors.New("invalid status line")
	}
	status, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, "", err
	}

	contentLen := 0
	for {
		line, err = r.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])
		if key == "content-length" {
			n, convErr := strconv.Atoi(val)
			if convErr != nil {
				return 0, "", convErr
			}
			contentLen = n
		}
	}

	if contentLen > 0 {
		buf := make([]byte, contentLen)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, "", err
		}
		body = string(buf)
	}
	return status, body, nil
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
