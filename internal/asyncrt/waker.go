package asyncrt

// WakerKind identifies a wait queue category.
type WakerKind uint8

const (
	// WakerInvalid indicates an invalid waker key.
	WakerInvalid WakerKind = iota
	// WakerJoin indicates a join wait queue.
	WakerJoin
	// WakerChannelRecv indicates a channel receive wait queue.
	WakerChannelRecv
	// WakerChannelSend indicates a channel send wait queue.
	WakerChannelSend
	// WakerTimer indicates a timer wait queue.
	WakerTimer
	// WakerSelect indicates a select wait queue.
	WakerSelect
	// WakerNetAccept indicates a network accept wait queue.
	WakerNetAccept
	// WakerNetRead indicates a network readable wait queue.
	WakerNetRead
	// WakerNetWrite indicates a network writable wait queue.
	WakerNetWrite
)

// WakerKey identifies a wait queue entry.
type WakerKey struct {
	Kind WakerKind
	A    uint64
	B    uint64
}

// IsValid reports whether the key is usable for waiting.
func (k WakerKey) IsValid() bool {
	return k.Kind != WakerInvalid
}

// JoinKey builds a join wait key for a target task.
func JoinKey(target TaskID) WakerKey {
	return WakerKey{Kind: WakerJoin, A: uint64(target)}
}

// ChannelRecvKey builds a wait key for channel receivers.
func ChannelRecvKey(channelID ChannelID) WakerKey {
	return WakerKey{Kind: WakerChannelRecv, A: uint64(channelID)}
}

// ChannelSendKey builds a wait key for channel senders.
func ChannelSendKey(channelID ChannelID) WakerKey {
	return WakerKey{Kind: WakerChannelSend, A: uint64(channelID)}
}

// TimerKey builds a wait key for a timer.
func TimerKey(timerID TimerID) WakerKey {
	return WakerKey{Kind: WakerTimer, A: uint64(timerID)}
}

// SelectKey builds a wait key for a select operation.
func SelectKey(selectID SelectID) WakerKey {
	return WakerKey{Kind: WakerSelect, A: uint64(selectID)}
}

// NetAcceptKey builds a wait key for listener accept readiness.
func NetAcceptKey(fd int) WakerKey {
	if fd <= 0 {
		return WakerKey{Kind: WakerInvalid}
	}
	return WakerKey{Kind: WakerNetAccept, A: uint64(fd)}
}

// NetReadKey builds a wait key for connection readable readiness.
func NetReadKey(fd int) WakerKey {
	if fd <= 0 {
		return WakerKey{Kind: WakerInvalid}
	}
	return WakerKey{Kind: WakerNetRead, A: uint64(fd)}
}

// NetWriteKey builds a wait key for connection writable readiness.
func NetWriteKey(fd int) WakerKey {
	if fd <= 0 {
		return WakerKey{Kind: WakerInvalid}
	}
	return WakerKey{Kind: WakerNetWrite, A: uint64(fd)}
}

// Waiter represents a task waiting on a key (optionally as part of a select).
type Waiter struct {
	TaskID   TaskID
	SelectID SelectID
}

// PollOutcomeKind reports how a poll iteration completed.
type PollOutcomeKind uint8

const (
	// PollDoneSuccess indicates the task completed successfully.
	PollDoneSuccess PollOutcomeKind = iota
	// PollDoneCancelled indicates the task was cancelled.
	PollDoneCancelled
	// PollYielded indicates the task yielded.
	PollYielded
	// PollParked indicates the task is parked.
	PollParked
)

// PollOutcome describes the outcome of polling a task once.
type PollOutcome struct {
	Kind    PollOutcomeKind
	Value   any
	ParkKey WakerKey
}
