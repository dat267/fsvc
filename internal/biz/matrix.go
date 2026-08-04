package biz

// PriorityByUrgencyImpact is the standard Freshservice priority matrix:
// rows are urgency (1=Low..3=High), columns are impact (1=Low..3=High),
// cells are priority (1=Low..4=Urgent).
var PriorityByUrgencyImpact = map[int]map[int]int{
	1: {1: 1, 2: 1, 3: 2},
	2: {1: 1, 2: 2, 3: 3},
	3: {1: 2, 2: 3, 3: 4},
}

// MinUrgencyImpactForPriority returns the minimum (urgency, impact) pair that
// still maps to the given priority in the matrix, preferring to raise urgency
// over impact (impact is minimized first). ok is false for unknown priorities.
func MinUrgencyImpactForPriority(priority int) (urgency, impact int, ok bool) {
	switch priority {
	case 1:
		return 1, 1, true
	case 2:
		return 3, 1, true
	case 3:
		return 3, 2, true
	case 4:
		return 3, 3, true
	default:
		return 0, 0, false
	}
}

// PriorityFor returns the priority for a given urgency and impact, or 0 when
// the pair is unknown.
func PriorityFor(urgency, impact int) int {
	return PriorityByUrgencyImpact[urgency][impact]
}
