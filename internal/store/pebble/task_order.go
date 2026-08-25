package pebble

import (
	"sort"

	"github.com/Homiakus/axiom/internal/runtime"
)

func canonicalTaskLess(left, right *runtime.ActivityTask) bool {
	leftSeq := taskSeqFromID(left.ID)
	rightSeq := taskSeqFromID(right.ID)
	if leftSeq > 0 && rightSeq > 0 && leftSeq != rightSeq {
		return leftSeq < rightSeq
	}
	if leftSeq > 0 && rightSeq == 0 {
		return true
	}
	if leftSeq == 0 && rightSeq > 0 {
		return false
	}
	return left.ID < right.ID
}

func sortTasksCanonical(tasks []*runtime.ActivityTask) {
	sort.SliceStable(tasks, func(i, j int) bool {
		return canonicalTaskLess(tasks[i], tasks[j])
	})
}
