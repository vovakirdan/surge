package vm

import (
	"golang.org/x/sys/unix"
)

func netFdReady(fd int, wantWrite bool) (bool, error) {
	if fd <= 0 {
		return true, nil
	}
	const maxFD = int(^uint32(0) >> 1)
	if fd > maxFD {
		return true, unix.EINVAL
	}
	events := int16(unix.POLLIN)
	readyMask := int16(unix.POLLIN | unix.POLLHUP | unix.POLLERR)
	if wantWrite {
		events = unix.POLLOUT
		readyMask = unix.POLLOUT | unix.POLLHUP | unix.POLLERR
	}
	pfds := []unix.PollFd{{Fd: int32(fd), Events: events}} //nolint:gosec // bounded by maxFD.
	for {
		n, err := unix.Poll(pfds, 0)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return true, err
		}
		if n == 0 {
			return false, nil
		}
		return pfds[0].Revents&readyMask != 0, nil
	}
}
