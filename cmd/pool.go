package cmd

// poolSize clamps the configured concurrency to the amount of work available,
// so a small batch never spins up idle workers. It returns at least 1 for any
// non-empty batch and 0 only for an empty one.
func poolSize(items int) int {
	workers := concurrency
	if workers < 1 {
		workers = 1
	}
	if items < workers {
		workers = items
	}
	return workers
}
