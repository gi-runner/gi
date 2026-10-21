package match

import (
	"math"
	"strings"

	"github.com/gi-runner/gi/internal/value"
)

// Normalization makes equal finite numeric literals use the same Go type.
// Schemas and composite values still use one-to-one graph matching.
func literalBagEqual(actual, expected []any) (equal, handled bool) {
	for _, item := range expected {
		if !literalKey(item) {
			return false, false
		}
		if s, ok := item.(string); ok && strings.HasPrefix(s, "#") {
			return false, false
		}
	}
	for _, item := range actual {
		if !literalKey(item) {
			return false, false
		}
	}
	counts := make(map[any]int, len(actual))
	for _, item := range actual {
		counts[literalValue(item)]++
	}
	for _, item := range expected {
		item = literalValue(item)
		if counts[item] == 0 {
			return false, true
		}
		counts[item]--
	}
	return true, true
}

func literalValue(v any) any {
	// DataValue providers may supply integral floats without prior normalization.
	if _, ok := v.(float64); ok {
		return value.Normalize(v)
	}
	return v
}

func literalKey(v any) bool {
	switch x := v.(type) {
	case nil, bool, string, int64:
		return true
	case float64:
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	default:
		return false
	}
}

// Greedy pairing handles easy graphs; layered augmenting paths resolve
// overlapping schemas in O(E sqrt(V)) rather than repeatedly exploring cycles.
func perfectMatching(edges [][]int, actuals int) bool {
	left := make([]int, len(edges))
	owner := make([]int, actuals)
	for i := range left {
		left[i] = -1
	}
	for j := range owner {
		owner[j] = -1
	}
	paired := 0
	for i, neighbors := range edges {
		for _, j := range neighbors {
			if owner[j] < 0 {
				left[i], owner[j] = j, i
				paired++
				break
			}
		}
	}
	if paired == len(edges) {
		return true
	}
	distance := make([]int, len(edges))
	queue := make([]int, 0, len(edges))
	const infinity = int(^uint(0) >> 1)
	for paired < len(edges) {
		queue = queue[:0]
		for i, j := range left {
			distance[i] = infinity
			if j < 0 {
				distance[i] = 0
				queue = append(queue, i)
			}
		}
		shortest := infinity
		for head := 0; head < len(queue); head++ {
			i := queue[head]
			if distance[i] >= shortest {
				continue
			}
			for _, j := range edges[i] {
				previous := owner[j]
				if previous < 0 {
					shortest = distance[i] + 1
				} else if distance[previous] == infinity {
					distance[previous] = distance[i] + 1
					queue = append(queue, previous)
				}
			}
		}
		if shortest == infinity {
			return false
		}
		var augment func(int) bool
		augment = func(i int) bool {
			for _, j := range edges[i] {
				previous := owner[j]
				if previous < 0 {
					if distance[i]+1 != shortest {
						continue
					}
				} else if distance[previous] != distance[i]+1 || !augment(previous) {
					continue
				}
				left[i], owner[j] = j, i
				return true
			}
			distance[i] = infinity
			return false
		}
		for i, j := range left {
			if j < 0 && augment(i) {
				paired++
			}
		}
	}
	return true
}
