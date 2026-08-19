package asyncrt

// SpawnSleep registers a sleep task and enqueues it.
func (e *Executor[P]) SpawnSleep(state TaskState) TaskID {
	return e.spawnBuiltin(TaskKindSleep, state, false)
}
