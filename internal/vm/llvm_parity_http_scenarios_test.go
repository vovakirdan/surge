package vm_test

// The HTTP scenarios the parity cases drive: a client that speaks the protocol,
// kept apart from the harness that compares one lane against the other. The MT
// suite splits the same way (mt_correctness_http_clients_test.go).

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

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
