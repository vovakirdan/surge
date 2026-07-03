package vm_test

import (
	"bytes"
	"io"
	"net"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

const runtimeV2AcceptPingSource = `import stdlib/net as net;

fn pong() -> byte[] {
    let mut out: byte[] = [];
    out.push(80:byte);
    out.push(79:byte);
    out.push(78:byte);
    out.push(71:byte);
    out.push(10:byte);
    return out;
}

async fn serve_one(listener: TcpListener, count: uint) -> int {
    let accept_task = net.accept(&listener).await();
    compare accept_task {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let mut seen: uint = 0:uint;
                    while seen < count {
                        let read_task = net.read_some(&conn, 16:uint).await();
                        let read_ok: bool = compare read_task {
                            Success(read_res) => compare read_res {
                                Success(bytes) => bytes.__len() != 0:uint;
                                _ => false;
                            };
                            Cancelled() => false;
                        };
                        if !read_ok {
                            let _ = net.close_conn(own conn);
                            return 2;
                        }
                        let write_task = net.write_all(&conn, pong()).await();
                        let write_ok: bool = compare write_task {
                            Success(write_res) => compare write_res {
                                Success(_) => true;
                                _ => false;
                            };
                            Cancelled() => false;
                        };
                        if !write_ok {
                            let _ = net.close_conn(own conn);
                            return 3;
                        }
                        seen = seen + 1:uint;
                    }
                    let _ = net.close_conn(own conn);
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}

@entrypoint("argv")
fn main(port: uint, count: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let result = serve_one(listener, count).await();
            return compare result {
                Success(code) => code;
                Cancelled() => 99;
            };
        }
        _ => return 1;
    };
    return 1;
}
`

type runtimeV2AcceptServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	outBuf *bytes.Buffer
	errBuf *bytes.Buffer
	waitCh chan error
}

func startRuntimeV2AcceptServer(
	t *testing.T,
	source string,
	env []string,
	args ...string,
) *runtimeV2AcceptServer {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath, args...)
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Runtime V2 accept fixture: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	return &runtimeV2AcceptServer{t: t, cmd: cmd, outBuf: &outBuf, errBuf: &errBuf, waitCh: waitCh}
}

func (s *runtimeV2AcceptServer) fail(format string, args ...any) {
	s.t.Helper()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.waitCh
	s.t.Fatalf(format+"\nstdout:\n%s\nstderr:\n%s", append(args, s.outBuf.String(), s.errBuf.String())...)
}

func (s *runtimeV2AcceptServer) waitExitZero(timeout time.Duration) (string, string) {
	s.t.Helper()
	select {
	case err := <-s.waitCh:
		if err != nil {
			s.t.Fatalf("Runtime V2 accept fixture exit: %v\nstdout:\n%s\nstderr:\n%s",
				err, s.outBuf.String(), s.errBuf.String())
		}
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-s.waitCh
		s.t.Fatalf("Runtime V2 accept fixture timed out\nstdout:\n%s\nstderr:\n%s",
			s.outBuf.String(), s.errBuf.String())
	}
	return s.outBuf.String(), s.errBuf.String()
}

func runtimeV2AcceptEnv(t *testing.T) []string {
	t.Helper()
	return envWithStdlib(repoRoot(t))
}

func TestRuntimeV2AcceptShardOneNativeNetCompatibility(t *testing.T) {
	ensureLLVMToolchain(t)

	port := pickFreePort(t)
	env := overrideEnvVar(runtimeV2AcceptEnv(t), "SURGE_SHARDS", "1")
	srv := startRuntimeV2AcceptServer(
		t,
		runtimeV2AcceptPingSource,
		env,
		strconv.Itoa(port),
		"4",
	)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial accept compatibility fixture: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	for i := range 4 {
		if _, err = conn.Write([]byte("PING\n")); err != nil {
			_ = conn.Close()
			srv.fail("write ping %d: %v", i, err)
		}
		var buf [5]byte
		if _, err = io.ReadFull(conn, buf[:]); err != nil {
			_ = conn.Close()
			srv.fail("read pong %d: %v", i, err)
		}
		if string(buf[:]) != "PONG\n" {
			_ = conn.Close()
			srv.fail("unexpected response %d: %q", i, string(buf[:]))
		}
	}
	_ = conn.Close()

	srv.waitExitZero(10 * time.Second)
}
