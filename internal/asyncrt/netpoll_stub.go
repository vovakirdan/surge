//go:build !linux

package asyncrt

func (e *Executor) netPoll(timeoutMs int64) bool {
	return false
}
