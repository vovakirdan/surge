package asyncrt

// SpawnTimeout registers a timeout task and enqueues it.
func (e *Executor) SpawnTimeout(state any) TaskID {
	return e.spawnBuiltin(TaskKindTimeout, state, true)
}
