package asyncrt

// SpawnNetAccept registers a network accept wait task and enqueues it.
func (e *Executor) SpawnNetAccept(state any) TaskID {
	return e.spawnBuiltin(TaskKindNetAccept, state, false)
}

// SpawnNetRead registers a network readable wait task and enqueues it.
func (e *Executor) SpawnNetRead(state any) TaskID {
	return e.spawnBuiltin(TaskKindNetRead, state, false)
}

// SpawnNetWrite registers a network writable wait task and enqueues it.
func (e *Executor) SpawnNetWrite(state any) TaskID {
	return e.spawnBuiltin(TaskKindNetWrite, state, false)
}
