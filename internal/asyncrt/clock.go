package asyncrt

import (
	"math"
	"time"

	"fortio.org/safecast"
)

// TimerMode controls whether timers use virtual or real time.
type TimerMode uint8

const (
	TimerModeVirtual TimerMode = iota
	TimerModeReal
)

// Clock supplies time and blocking behavior for timers.
type Clock interface {
	NowMs() uint64
	SleepUntilMs(deadlineMs uint64)
}

// VirtualClock advances executor time without blocking.
type VirtualClock struct {
	ex *Executor
}

func (c *VirtualClock) NowMs() uint64 {
	if c == nil || c.ex == nil {
		return 0
	}
	return c.ex.nowMs
}

func (c *VirtualClock) SleepUntilMs(deadlineMs uint64) {
	if c == nil || c.ex == nil {
		return
	}
	c.ex.nowMs = deadlineMs
}

// RealClock blocks the OS thread until the requested deadline.
// It relies on NowFunc for monotonic time.
type RealClock struct {
	NowFunc func() uint64
}

func (c *RealClock) NowMs() uint64 {
	if c == nil || c.NowFunc == nil {
		return 0
	}
	return c.NowFunc()
}

func (c *RealClock) SleepUntilMs(deadlineMs uint64) {
	if c == nil {
		return
	}
	now := c.NowMs()
	if deadlineMs <= now {
		return
	}
	delta := deadlineMs - now
	maxMs := uint64(math.MaxInt64 / int64(time.Millisecond))
	if delta > maxMs {
		delta = maxMs
	}
	delay, err := safecast.Conv[int64](delta)
	if err != nil {
		return
	}
	time.Sleep(time.Duration(delay) * time.Millisecond)
}
