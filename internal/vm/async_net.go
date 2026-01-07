package vm

import (
	"golang.org/x/sys/unix"

	"surge/internal/asyncrt"
)

type netWaitState struct {
	fd int
}

func (vm *VM) pollNetWaitTask(task *asyncrt.Task) (asyncrt.PollOutcome, *VMError) {
	if vm == nil || task == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "missing net wait task")
	}
	state, ok := task.State.(*netWaitState)
	if !ok || state == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "net wait state missing")
	}
	if task.Cancelled {
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneCancelled}, nil
	}
	if state.fd <= 0 {
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
	}
	wantWrite := task.Kind == asyncrt.TaskKindNetWrite
	ready, err := netFdReady(state.fd, wantWrite)
	if err != nil || ready {
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
	}
	var key asyncrt.WakerKey
	switch task.Kind {
	case asyncrt.TaskKindNetAccept:
		key = asyncrt.NetAcceptKey(state.fd)
	case asyncrt.TaskKindNetRead:
		key = asyncrt.NetReadKey(state.fd)
	case asyncrt.TaskKindNetWrite:
		key = asyncrt.NetWriteKey(state.fd)
	default:
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
	}
	if !key.IsValid() {
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
	}
	return asyncrt.PollOutcome{Kind: asyncrt.PollParked, ParkKey: key}, nil
}

func netFdReady(fd int, wantWrite bool) (bool, error) {
	if fd <= 0 {
		return true, nil
	}
	events := int16(unix.POLLIN)
	readyMask := int16(unix.POLLIN | unix.POLLHUP | unix.POLLERR)
	if wantWrite {
		events = unix.POLLOUT
		readyMask = unix.POLLOUT | unix.POLLHUP | unix.POLLERR
	}
	pfds := []unix.PollFd{{Fd: int32(fd), Events: events}}
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
