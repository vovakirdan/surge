package asyncrt

// WakerKind identifies a wait queue category.
type WakerKind uint8

const (
	// WakerInvalid indicates an invalid waker key.
	WakerInvalid WakerKind = iota
	// WakerJoin indicates a join wait queue.
	WakerJoin
	WakerChannelRecv
	WakerChannelSend
	WakerTimer
	WakerSelect
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
	PollYielded
	PollParked
)

// PollOutcome describes the outcome of polling a task once.
type PollOutcome struct {
	Kind    PollOutcomeKind
	Value   any
	ParkKey WakerKey
}
