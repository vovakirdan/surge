package asyncrt

// SpawnSleep registers a sleep task and enqueues it.
func (e *Executor) SpawnSleep(state any) TaskID {
	return e.spawnBuiltin(TaskKindSleep, state, false)
}
