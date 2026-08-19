package asyncrt

// SpawnTimeout registers a timeout task and enqueues it.
func (e *Executor[P]) SpawnTimeout(state TaskState) TaskID {
	return e.spawnBuiltin(TaskKindTimeout, state, true)
}
