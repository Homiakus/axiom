package runtime

type RuleQueue struct {
	plan   *fastPlan
	queued bitset
	ring   []int
	head   int
}

func newRuleQueue(plan *fastPlan, names []string) RuleQueue {
	q := RuleQueue{plan: plan}
	if plan != nil {
		q.queued = newBitset(len(plan.rules))
	}
	q.PushNames(names)
	return q
}

func (q *RuleQueue) PushNames(names []string) {
	for _, name := range names {
		q.PushName(name)
	}
}

func (q *RuleQueue) PushName(name string) {
	if q.plan == nil {
		return
	}
	if id, ok := q.plan.ruleIDs[name]; ok {
		q.PushID(id)
	}
}

func (q *RuleQueue) PushID(id int) {
	if q.plan == nil || id < 0 || id >= len(q.plan.rules) {
		return
	}
	if q.queued.has(id) {
		return
	}
	q.queued.set(id)
	q.ring = append(q.ring, id)
}

func (q *RuleQueue) Pop() (int, bool) {
	for q.head < len(q.ring) {
		id := q.ring[q.head]
		q.head++
		if id < 0 || q.plan == nil || id >= len(q.plan.rules) {
			continue
		}
		q.queued.clear(id)
		return id, true
	}
	return 0, false
}

func (q RuleQueue) Empty() bool {
	return q.head >= len(q.ring)
}
