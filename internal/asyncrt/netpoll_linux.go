//go:build linux

package asyncrt

import "golang.org/x/sys/unix"

type netPollEntry struct {
	fd        int32
	events    int16
	readKeys  []WakerKey
	writeKeys []WakerKey
}

func (e *Executor) netPoll(timeoutMs int64) bool {
	if e == nil || len(e.waiters) == 0 {
		return false
	}
	entries := make(map[int32]*netPollEntry)
	const maxFD = int32(^uint32(0) >> 1)
	for key := range e.waiters {
		var events int16
		switch key.Kind {
		case WakerNetAccept, WakerNetRead:
			events = unix.POLLIN
		case WakerNetWrite:
			events = unix.POLLOUT
		default:
			continue
		}
		if key.A > uint64(maxFD) {
			continue
		}
		fd := int32(key.A) //nolint:gosec // bounded by maxFD and Net*Key uses int fd.
		if fd <= 0 {
			continue
		}
		entry := entries[fd]
		if entry == nil {
			entry = &netPollEntry{fd: fd}
			entries[fd] = entry
		}
		entry.events |= events
		if events == unix.POLLOUT {
			entry.writeKeys = append(entry.writeKeys, key)
		} else {
			entry.readKeys = append(entry.readKeys, key)
		}
	}
	if len(entries) == 0 {
		return false
	}

	pfds := make([]unix.PollFd, 0, len(entries))
	refs := make([]*netPollEntry, 0, len(entries))
	for _, entry := range entries {
		pfds = append(pfds, unix.PollFd{Fd: entry.fd, Events: entry.events})
		refs = append(refs, entry)
	}

	timeout := 0
	maxTimeout := int64(^uint(0) >> 1)
	switch {
	case timeoutMs < 0:
		timeout = -1
	case timeoutMs > maxTimeout:
		timeout = int(maxTimeout)
	default:
		timeout = int(timeoutMs)
	}

	for {
		n, err := unix.Poll(pfds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			woke := false
			for _, entry := range refs {
				for _, key := range entry.readKeys {
					e.WakeKeyAll(key)
					woke = true
				}
				for _, key := range entry.writeKeys {
					e.WakeKeyAll(key)
					woke = true
				}
			}
			return woke
		}
		if n == 0 {
			return false
		}
		break
	}

	woke := false
	for i, pfd := range pfds {
		if pfd.Revents == 0 {
			continue
		}
		entry := refs[i]
		readReady := pfd.Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0
		writeReady := pfd.Revents&(unix.POLLOUT|unix.POLLHUP|unix.POLLERR) != 0
		if readReady {
			for _, key := range entry.readKeys {
				e.WakeKeyAll(key)
				woke = true
			}
		}
		if writeReady {
			for _, key := range entry.writeKeys {
				e.WakeKeyAll(key)
				woke = true
			}
		}
	}
	return woke
}
