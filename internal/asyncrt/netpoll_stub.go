//go:build !linux

package asyncrt

func (e *Executor[P]) netPoll(timeoutMs int64) bool {
	return false
}
