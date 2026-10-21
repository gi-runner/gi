package match

import "testing"

func TestPerfectMatchingAgainstExhaustiveOracle(t *testing.T) {
	const size = 4
	for mask := range 1 << (size * size) {
		edges := make([][]int, size)
		for i := range size {
			for j := range size {
				if mask&(1<<(i*size+j)) != 0 {
					edges[i] = append(edges[i], j)
				}
			}
		}
		var possible func(int, uint) bool
		possible = func(i int, used uint) bool {
			if i == size {
				return true
			}
			for _, j := range edges[i] {
				if used&(1<<j) == 0 && possible(i+1, used|1<<j) {
					return true
				}
			}
			return false
		}
		if got, want := perfectMatching(edges, size), possible(0, 0); got != want {
			t.Fatalf("graph %v: got %v, want %v", edges, got, want)
		}
	}
}
