package vm_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// The HTTP client scenarios the multi-threaded correctness tests drive a
// server with.
//
// They are clients, not assertions: each one speaks the protocol and reports
// whether the exchange completed, leaving the test above to decide what that
// means. Keeping them here stops the protocol details from crowding out the
// concurrency questions the tests are actually asking.

func runMTHTTPKeepaliveScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	reader := bufio.NewReader(conn)

	req := "GET /fast HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write keepalive req1: %w", err)
	}
	status, body, err := readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp1: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("keepalive resp1 mismatch: status=%d body=%q", status, body)
	}

	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write keepalive req2: %w", err)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp2: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("keepalive resp2 mismatch: status=%d body=%q", status, body)
	}
	return nil
}

func runMTHTTPConcurrentScenario(addr string, clients int) error {
	var wg sync.WaitGroup
	errCh := make(chan error, clients)
	for i := range clients {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
			if err != nil {
				errCh <- fmt.Errorf("dial %d: %w", id, err)
				return
			}
			defer conn.Close()
			if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				errCh <- fmt.Errorf("set deadline %d: %w", id, err)
				return
			}
			reader := bufio.NewReader(conn)
			path := "/fast"
			if id%10 == 0 {
				path = "/slow"
			}
			req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: example\r\n\r\n", path)
			if _, err = conn.Write([]byte(req)); err != nil {
				errCh <- fmt.Errorf("write %d: %w", id, err)
				return
			}
			status, body, err := readHTTPResponse(reader)
			if err != nil {
				errCh <- fmt.Errorf("read %d: %w", id, err)
				return
			}
			if status != 200 || body != "ok" {
				errCh <- fmt.Errorf("resp %d mismatch: status=%d body=%q", id, status, body)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func runMTHTTPPostScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial small: %w", err)
	}
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set small deadline: %w", err)
	}
	reader := bufio.NewReader(conn)
	body := "abcdefgh"
	req := fmt.Sprintf("POST /upload HTTP/1.1\r\nHost: example\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	if _, err = conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write small body: %w", err)
	}
	status, respBody, err := readHTTPResponse(reader)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read small body: %w", err)
	}
	if status != 200 || respBody != "ok" {
		_ = conn.Close()
		return fmt.Errorf("small body mismatch: status=%d body=%q", status, respBody)
	}
	_ = conn.Close()

	largeConn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial large: %w", err)
	}
	defer largeConn.Close()
	if err = largeConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set large deadline: %w", err)
	}
	largeReader := bufio.NewReader(largeConn)
	largeBody := strings.Repeat("a", 32)
	largeReq := fmt.Sprintf("POST /upload HTTP/1.1\r\nHost: example\r\nContent-Length: %d\r\n\r\n%s", len(largeBody), largeBody)
	if _, err = largeConn.Write([]byte(largeReq)); err != nil {
		return fmt.Errorf("write large body: %w", err)
	}
	status, respBody, err = readHTTPResponse(largeReader)
	if err != nil {
		return fmt.Errorf("read large body: %w", err)
	}
	if status != 413 || respBody != "" {
		return fmt.Errorf("large body mismatch: status=%d body=%q", status, respBody)
	}
	if err := largeConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("set close deadline: %w", err)
	}
	if _, err := largeReader.ReadByte(); err == nil {
		return fmt.Errorf("expected connection close after body limit, got byte")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected connection close after body limit: %w", err)
	}
	return nil
}
