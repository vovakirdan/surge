package vm_test

import (
	"strconv"
	"strings"
	"testing"
)

func TestBorrowedTcpConnAsyncHelperRunsWithoutVMPanic(t *testing.T) {
	requireVMBackend(t)

	port := pickFreePort(t)
	res := runProgramFromSource(t, `import stdlib/net as net;

fn pong_line() -> byte[] {
    let mut out: byte[] = [];
    let view = "PONG".bytes();
    let mut i: int = 0;
    while i < view.__len() to int {
        out.push(view[i]);
        i = i + 1;
    }
    out.push(10:byte);
    return out;
}

async fn send_line(conn: &TcpConn, data: byte[]) -> nothing {
    let _ = net.write_all(conn, data).await();
}

async fn server_once(listener: TcpListener) -> int {
    let accept_res = net.accept(&listener).await();
    let _ = net.close_listener(own listener);
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let data: byte[] = pong_line();
                    let _ = send_line(&conn, data).await();
                    let _ = net.close_conn(own conn);
                    return 0;
                }
                _ => return 2;
            };
        }
        Cancelled() => return 3;
    };
    return 4;
}

@entrypoint("argv")
fn main(port: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let server_task: Task<int> = @local spawn server_once(listener);
            let conn_res = net.connect("127.0.0.1", port);
            compare conn_res {
                Success(conn) => {
                    let read_res = net.read_some(&conn, 5:uint).await();
                    let _ = net.close_conn(own conn);
                    let _ = server_task.await();
                    compare read_res {
                        Success(_) => return 0;
                        Cancelled() => return 5;
                    };
                }
                _ => {
                    let _ = server_task.await();
                    return 6;
                }
            };
        }
        _ => return 7;
    };
    return 8;
}
`, runOptions{argv: []string{strconv.Itoa(port)}})

	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stderr, "invalid local id") || strings.Contains(res.stderr, "used after move") {
		t.Fatalf("expected no async borrow VM panic, got:\n%s", res.stderr)
	}
}

func TestBorrowedIntAsyncHelperRunsAfterSuspend(t *testing.T) {
	requireVMBackend(t)

	res := runProgramFromSource(t, `async fn read_after_wait(x: &int) -> int {
    checkpoint().await();
    return *x;
}

@entrypoint("argv")
fn main() -> int {
    let value: int = 2;
    compare read_after_wait(&value).await() {
        Success(out) => return out;
        Cancelled() => return 9;
    };
}
`, runOptions{})

	if res.exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stderr, "invalid local id") || strings.Contains(res.stderr, "used after move") {
		t.Fatalf("expected no async borrow VM panic, got:\n%s", res.stderr)
	}
}
