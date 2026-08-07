package cmd

var priorityMatrix = [4][4]int{
	1: {1: 1, 2: 1, 3: 2},
	2: {1: 1, 2: 2, 3: 3},
	3: {1: 2, 2: 3, 3: 4},
}

// MinUrgencyImpactForPriority returns the minimum (urgency, impact) pair that
// still maps to the given priority in the matrix, preferring to raise urgency
// over impact (impact is minimized first). ok is false for unknown priorities.
func MinUrgencyImpactForPriority(priority int) (urgency, impact int, ok bool) {
	for i := 1; i <= 3; i++ {
		for u := 1; u <= 3; u++ {
			if priorityMatrix[u][i] == priority {
				return u, i, true
			}
		}
	}
	return 0, 0, false
}

// PriorityFor returns the priority for a given urgency and impact, or 0 when
// the pair is unknown.
func PriorityFor(urgency, impact int) int {
	if urgency < 1 || urgency > 3 || impact < 1 || impact > 3 {
		return 0
	}
	return priorityMatrix[urgency][impact]
}
