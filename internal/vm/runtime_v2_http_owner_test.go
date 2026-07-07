//go:build runtime_v2_pending

package vm_test

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const runtimeV2HTTPOwnerLocalSource = `import stdlib/http as http;

fn string_to_bytes(s: &string) -> byte[] {
    let view = s.bytes();
    let length: uint = view.__len();
    let mut out: byte[] = [];
    out.reserve(length);
    let len_i: int = length to int;
    let mut i: int = 0;
    while i < len_i {
        let b: byte = view[i];
        out.push(b);
        i = i + 1;
    }
    return out;
}

async fn handle(req: http.Request) -> http.Response {
    let _ = req;
    let bytes = string_to_bytes(&"ok");
    return { status = 200, headers = [], body = http.Bytes(bytes) };
}

@entrypoint("argv")
fn main(port: uint, workers: uint) -> int {
    let cfg: http.ServerConfig = {
        max_pipeline_depth = 4:uint,
        worker_count = workers,
        max_initial_line_bytes = 1024:uint,
        max_header_bytes = 4096:uint,
        max_headers_count = 64:uint,
        max_body_bytes = 16:uint,
        accept_timeout_ms = 0:uint,
        idle_timeout_ms = 1000:uint,
        read_timeout_ms = 1000:uint,
        write_timeout_ms = 1000:uint
    };
    let handler: http.Handler = handle;
    let server_res = http.serve("127.0.0.1", port, cfg, handler).await();
    return compare server_res {
        Success(_) => 0;
        Cancelled() => 99;
    };
}
`

func TestRuntimeV2HTTPOwnerLocalStaticShape(t *testing.T) {
	root := repoRoot(t)
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "stdlib", "http", name))
		if err != nil {
			t.Fatalf("read stdlib/http/%s: %v", name, err)
		}
		return string(data)
	}
	serverSource := read("server.sg")
	acceptSource := read("accept.sg")
	combinedSource := serverSource + "\n" + acceptSource
	forbidden := []string{
		"Channel<int>",
		"make_channel::<int>",
		"conn.__opaque",
		"TcpConn = { __opaque",
		"serve_worker(",
		"spawn net.accept",
		"spawn net.read_some",
		"spawn net.write_all",
	}
	for _, needle := range forbidden {
		if strings.Contains(combinedSource, needle) {
			t.Fatalf("stdlib/http accept/server path must not use raw TcpConn handle handoff; found %q", needle)
		}
	}
	required := []struct {
		name   string
		source string
	}{
		{name: "serve_accept_worker", source: acceptSource},
		{name: "copy_listener(&listener)", source: serverSource},
		{name: "@local spawn serve_accept_worker", source: serverSource},
		{name: "let accept_task = net.accept(&listener)", source: acceptSource},
		{name: "let read_task = net.read_some(&conn, cap)", source: serverSource},
		{name: "let write_task = net.write_all(&conn, bytes)", source: serverSource},
	}
	for _, req := range required {
		if !strings.Contains(req.source, req.name) {
			t.Fatalf("stdlib/http accept/server path must keep HTTP work owner-local; missing %q", req.name)
		}
	}
}

func runRuntimeV2HTTPOwnerRequest(addr string, id int) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("client %d dial: %w", id, err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("client %d deadline: %w", id, err)
	}
	req := fmt.Sprintf("GET /%d HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", id)
	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("client %d write: %w", id, err)
	}
	status, body, err := readHTTPResponse(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("client %d read response: %w", id, err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("client %d response mismatch: status=%d body=%q", id, status, body)
	}
	return nil
}

func stopRuntimeV2HTTPOwnerServer(t *testing.T, srv *runtimeV2AcceptServer) {
	t.Helper()
	select {
	case err := <-srv.waitCh:
		t.Fatalf("HTTP owner-local fixture exited before explicit stop: %v\nstdout:\n%s\nstderr:\n%s",
			err, srv.outBuf.String(), srv.errBuf.String())
	default:
	}
	if srv.cmd.Process != nil {
		_ = srv.cmd.Process.Kill()
	}
	<-srv.waitCh
}

func TestRuntimeV2HTTPOwnerLocalBehavior(t *testing.T) {
	ensureLLVMToolchain(t)

	for _, shards := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			const clients = 8
			port := pickFreePort(t)
			env := runtimeV2AcceptEnv(t)
			env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
			env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
			env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
			srv := startRuntimeV2AcceptServer(
				t,
				runtimeV2HTTPOwnerLocalSource,
				env,
				strconv.Itoa(port),
				strconv.Itoa(shards),
			)

			addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			if err := runRuntimeV2HTTPOwnerRequest(addr, -1); err != nil {
				srv.fail("HTTP owner-local warmup failed: %v", err)
			}
			errCh := make(chan error, clients)
			for i := 0; i < clients; i++ {
				go func(id int) {
					errCh <- runRuntimeV2HTTPOwnerRequest(addr, id)
				}(i)
			}
			for i := 0; i < clients; i++ {
				if err := <-errCh; err != nil {
					srv.fail("HTTP owner-local fixture failed: %v", err)
				}
			}
			stopRuntimeV2HTTPOwnerServer(t, srv)
		})
	}
}
