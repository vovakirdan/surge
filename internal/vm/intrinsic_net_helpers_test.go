package vm

import (
	"syscall"
	"testing"
)

func TestNetSetNoDelay(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	defer closeNetFD(fd)

	if noDelayErr := netSetNoDelay(fd); noDelayErr != nil {
		t.Fatalf("set TCP_NODELAY: %v", noDelayErr)
	}
	got, err := syscall.GetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	if err != nil {
		t.Fatalf("get TCP_NODELAY: %v", err)
	}
	if got != 1 {
		t.Fatalf("TCP_NODELAY = %d, want 1", got)
	}
}
