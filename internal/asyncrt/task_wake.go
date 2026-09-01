package asyncrt

// Wake enqueues a task if it is not done. Removing a channel receive wait also
// retires its exact detached rendezvous claim before retry subscribers run.
func (e *Executor[P]) Wake(id TaskID) {
	if e == nil {
		return
	}
	task := e.tasks[id]
	if task == nil || task.Status == TaskDone {
		return
	}
	var releasedClaim *Channel[P]
	if key, ok := e.parked[id]; ok {
		releasedClaim = e.retireRecvClaimForWake(id, key)
		e.removeWaiter(key, id)
		delete(e.parked, id)
	}
	e.enqueue(id)
	if releasedClaim != nil {
		releasedClaim.releaseRecvClaim(e)
	}
}
