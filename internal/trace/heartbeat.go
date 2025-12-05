package trace

import (
	"fmt"
	"sync"
	"time"
)

// Heartbeat periodically emits heartbeat events to detect when the compiler hangs.
// If no SpanEnd events are received but heartbeats continue, the compiler is likely stuck.
type Heartbeat struct {
	tracer   Tracer
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
	started  bool
	mu       sync.Mutex
}

// StartHeartbeat creates and starts a new heartbeat goroutine.
// The goroutine will emit heartbeat events at the specified interval.
func StartHeartbeat(tracer Tracer, interval time.Duration) *Heartbeat {
	if tracer == nil || !tracer.Enabled() || interval <= 0 {
		return nil
	}

	h := &Heartbeat{
		tracer:   tracer,
		interval: interval,
		stopCh:   make(chan struct{}),
	}

	h.mu.Lock()
	h.started = true
	h.mu.Unlock()

	h.wg.Add(1)
	go h.run()

	return h
}

// run is the main heartbeat loop that emits events periodically.
func (h *Heartbeat) run() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	seq := uint64(0)
	for {
		select {
		case <-ticker.C:
			seq++
			h.tracer.Emit(&Event{
				Time:   time.Now(),
				Seq:    NextSeq(),
				Kind:   KindHeartbeat,
				Scope:  ScopeDriver,
				GID:    getGoroutineID(),
				Name:   "heartbeat",
				Detail: fmt.Sprintf("#%d", seq),
			})
		case <-h.stopCh:
			return
		}
	}
}

// Stop gracefully stops the heartbeat goroutine and waits for it to finish.
func (h *Heartbeat) Stop() {
	if h == nil {
		return
	}

	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return
	}
	h.started = false
	h.mu.Unlock()

	close(h.stopCh)
	h.wg.Wait()
}
