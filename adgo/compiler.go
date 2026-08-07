package adgo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ValidationError struct {
	Code    string `json:"code"`
	Node    string `json:"node,omitempty"`
	Message string `json:"message"`
}

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, item := range e {
		if item.Node != "" {
			parts = append(parts, fmt.Sprintf("%s[%s]: %s", item.Code, item.Node, item.Message))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", item.Code, item.Message))
		}
	}
	return strings.Join(parts, "; ")
}

func Compile(def Definition) (*Plan, error) {
	errs := validateDefinition(def)
	if len(errs) > 0 {
		sort.SliceStable(errs, func(i, j int) bool {
			if errs[i].Code == errs[j].Code {
				return errs[i].Node < errs[j].Node
			}
			return errs[i].Code < errs[j].Code
		})
		return nil, errs
	}

	nodes := make(map[string]Node, len(def.Nodes))
	inbound := make(map[string][]string, len(def.Nodes))
	outbound := make(map[string][]Transition, len(def.Nodes))
	producers := map[string][]string{}
	incomingTransition := map[string]int{}
	for _, n := range def.Nodes {
		nodes[n.ID] = cloneNode(n)
		inbound[n.ID] = append([]string(nil), n.DependsOn...)
		outbound[n.ID] = append([]Transition(nil), n.Next...)
		for _, key := range n.Produces {
			producers[key] = append(producers[key], n.ID)
		}
		for _, tr := range n.Next {
			incomingTransition[tr.To]++
		}
	}
	entry := make([]string, 0)
	for _, n := range def.Nodes {
		if len(n.DependsOn) == 0 && incomingTransition[n.ID] == 0 {
			entry = append(entry, n.ID)
		}
	}
	sort.Strings(entry)

	canon := def
	canon.Nodes = append([]Node(nil), def.Nodes...)
	sort.Slice(canon.Nodes, func(i, j int) bool { return canon.Nodes[i].ID < canon.Nodes[j].ID })
	for i := range canon.Nodes {
		sort.Strings(canon.Nodes[i].DependsOn)
		sort.Strings(canon.Nodes[i].Requires)
		sort.Strings(canon.Nodes[i].Produces)
		sort.Strings(canon.Nodes[i].Writes)
		sort.Strings(canon.Nodes[i].ResourceKeys)
		sort.Strings(canon.Nodes[i].RequiredPermissions)
		sort.Slice(canon.Nodes[i].Next, func(a, b int) bool {
			if canon.Nodes[i].Next[a].To == canon.Nodes[i].Next[b].To {
				return canon.Nodes[i].Next[a].Outcome < canon.Nodes[i].Next[b].Outcome
			}
			return canon.Nodes[i].Next[a].To < canon.Nodes[i].Next[b].To
		})
	}
	raw, _ := json.Marshal(canon)
	sum := sha256.Sum256(raw)

	p := &Plan{
		ID: def.ID, Version: def.Version, Digest: "sha256:" + hex.EncodeToString(sum[:]), Nodes: nodes, Entry: entry,
		InitialData: setOf(def.InitialData), AllowedPermissions: setOf(def.AllowedPermissions),
		GlobalConcurrency: def.GlobalConcurrency, CapabilityLimits: cloneIntMap(def.CapabilityLimits), ActivityLimits: cloneIntMap(def.ActivityLimits), Metadata: cloneStringMap(def.Metadata),
		inbound: inbound, outbound: outbound, producers: producers,
	}
	p.descendants = computeDescendants(p)
	return p, nil
}

func validateDefinition(def Definition) ValidationErrors {
	var errs ValidationErrors
	if strings.TrimSpace(def.ID) == "" {
		errs = append(errs, ValidationError{Code: "ADG001", Message: "definition id is required"})
	}
	if strings.TrimSpace(def.Version) == "" {
		errs = append(errs, ValidationError{Code: "ADG002", Message: "definition version is required"})
	}
	if len(def.Nodes) == 0 {
		errs = append(errs, ValidationError{Code: "ADG003", Message: "at least one node is required"})
		return errs
	}
	if def.GlobalConcurrency < 0 {
		errs = append(errs, ValidationError{Code: "ADG004", Message: "globalConcurrency must be >= 0"})
	}

	byID := map[string]Node{}
	for _, n := range def.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			errs = append(errs, ValidationError{Code: "ADG010", Message: "node id is required"})
			continue
		}
		if _, ok := byID[n.ID]; ok {
			errs = append(errs, ValidationError{Code: "ADG011", Node: n.ID, Message: "duplicate node id"})
			continue
		}
		byID[n.ID] = n
	}
	allowed := setOf(def.AllowedPermissions)
	produced := setOf(def.InitialData)
	for _, n := range def.Nodes {
		for _, key := range n.Produces {
			produced[key] = struct{}{}
		}
	}

	for _, n := range def.Nodes {
		switch n.Kind {
		case NodeActivity, NodeSubflow, NodeCompensation:
			if n.Activity == "" && n.Capability == "" {
				errs = append(errs, ValidationError{Code: "ADG020", Node: n.ID, Message: "activity or capability is required"})
			}
		case NodeDecision:
			if n.Activity == "" {
				errs = append(errs, ValidationError{Code: "ADG026", Node: n.ID, Message: "decision handler name is required"})
			}
		case NodeGate:
			if n.Activity == "" && n.Gate == nil {
				errs = append(errs, ValidationError{Code: "ADG027", Node: n.ID, Message: "gate handler or declarative gate spec is required"})
			}
		case NodeFork, NodeJoin, NodeWait, NodeHuman:
		default:
			errs = append(errs, ValidationError{Code: "ADG021", Node: n.ID, Message: "unsupported node kind"})
		}
		for _, dep := range n.DependsOn {
			if _, ok := byID[dep]; !ok {
				errs = append(errs, ValidationError{Code: "ADG022", Node: n.ID, Message: "missing dependency " + dep})
			}
		}
		for _, tr := range n.Next {
			if _, ok := byID[tr.To]; !ok {
				errs = append(errs, ValidationError{Code: "ADG023", Node: n.ID, Message: "transition target does not exist: " + tr.To})
			}
			if !validOutcome(tr.Outcome) {
				errs = append(errs, ValidationError{Code: "ADG028", Node: n.ID, Message: "unsupported transition outcome: " + string(tr.Outcome)})
			}
		}
		for _, req := range n.Requires {
			if _, ok := produced[req]; !ok {
				errs = append(errs, ValidationError{Code: "ADG024", Node: n.ID, Message: "required data has no initial value or producer: " + req})
			}
		}
		for _, perm := range n.RequiredPermissions {
			if _, ok := allowed[perm]; !ok {
				errs = append(errs, ValidationError{Code: "ADG025", Node: n.ID, Message: "permission is not allowed by plan: " + perm})
			}
		}
		if n.ExternalEffect {
			if n.Timeout <= 0 {
				errs = append(errs, ValidationError{Code: "ADG030", Node: n.ID, Message: "external activity requires timeout"})
			}
			if strings.TrimSpace(n.IdempotencyKey) == "" {
				errs = append(errs, ValidationError{Code: "ADG031", Node: n.ID, Message: "external activity requires idempotency key template"})
			}
			if n.Retry.MaxAttempts <= 0 {
				errs = append(errs, ValidationError{Code: "ADG032", Node: n.ID, Message: "external activity requires bounded retry policy"})
			}
			if n.Risk >= RiskHigh && !n.Irreversible && n.Compensation == "" {
				errs = append(errs, ValidationError{Code: "ADG033", Node: n.ID, Message: "high-risk reversible effect requires compensation"})
			}
		}
		if n.Kind == NodeJoin {
			if n.Join == nil {
				errs = append(errs, ValidationError{Code: "ADG040", Node: n.ID, Message: "join spec is required"})
			} else if len(n.DependsOn) == 0 {
				errs = append(errs, ValidationError{Code: "ADG041", Node: n.ID, Message: "join requires dependencies"})
			} else if n.Join.Mode != JoinAll && n.Join.Mode != JoinAny && n.Join.Mode != JoinNOfM && n.Join.Mode != JoinQuorum {
				errs = append(errs, ValidationError{Code: "ADG046", Node: n.ID, Message: "unsupported join mode"})
			} else if (n.Join.Mode == JoinNOfM || n.Join.Mode == JoinQuorum) && (n.Join.Threshold <= 0 || n.Join.Threshold > len(n.DependsOn)) {
				errs = append(errs, ValidationError{Code: "ADG042", Node: n.ID, Message: "invalid join threshold"})
			}
		}
		if n.Kind == NodeWait && n.Wait == nil {
			errs = append(errs, ValidationError{Code: "ADG043", Node: n.ID, Message: "wait spec is required"})
		} else if n.Kind == NodeWait && n.Wait != nil && n.Wait.Duration <= 0 && n.Wait.EventType == "" {
			errs = append(errs, ValidationError{Code: "ADG047", Node: n.ID, Message: "wait requires duration or eventType"})
		}
		if n.Kind == NodeHuman && (n.Human == nil || n.Human.EventType == "") {
			errs = append(errs, ValidationError{Code: "ADG044", Node: n.ID, Message: "human node requires event type"})
		}
		if n.MaxFanout < 0 {
			errs = append(errs, ValidationError{Code: "ADG045", Node: n.ID, Message: "maxFanout must be >= 0"})
		}
		if n.Kind == NodeGate && n.Gate != nil {
			for _, root := range n.Gate.RepairFrom {
				rn, ok := byID[root]
				if !ok {
					errs = append(errs, ValidationError{Code: "ADG048", Node: n.ID, Message: "repair root does not exist: " + root})
					continue
				}
				if rn.Loop == nil || rn.Loop.MaxIterations <= 0 || rn.Loop.MaxCost <= 0 || rn.Loop.MaxDuration <= 0 || rn.Loop.Epsilon <= 0 {
					errs = append(errs, ValidationError{Code: "ADG049", Node: n.ID, Message: "repair root " + root + " requires a complete loop bound"})
				}
			}
		}
	}

	incomingTransition := map[string]int{}
	for _, n := range def.Nodes {
		for _, tr := range n.Next {
			incomingTransition[tr.To]++
		}
	}
	roots := []string{}
	for _, n := range def.Nodes {
		if len(n.DependsOn) == 0 && incomingTransition[n.ID] == 0 {
			roots = append(roots, n.ID)
		}
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		n := byID[id]
		for _, tr := range n.Next {
			walk(tr.To)
		}
		for _, cand := range def.Nodes {
			for _, dep := range cand.DependsOn {
				if dep == id {
					walk(cand.ID)
				}
			}
		}
	}
	for _, r := range roots {
		walk(r)
	}
	terminalCount := 0
	for _, n := range def.Nodes {
		if !seen[n.ID] {
			errs = append(errs, ValidationError{Code: "ADG050", Node: n.ID, Message: "node is unreachable from any entry node"})
		}
		if len(n.Next) == 0 {
			terminalCount++
		}
	}
	if terminalCount == 0 {
		errs = append(errs, ValidationError{Code: "ADG051", Message: "plan has no terminal node"})
	}

	sccs := tarjan(def.Nodes)
	for _, scc := range sccs {
		cyclic := len(scc) > 1
		if len(scc) == 1 {
			n := byID[scc[0]]
			for _, tr := range n.Next {
				if tr.To == n.ID {
					cyclic = true
				}
			}
		}
		if !cyclic {
			continue
		}
		for _, id := range scc {
			b := byID[id].Loop
			if b == nil || b.MaxIterations <= 0 || b.MaxCost <= 0 || b.MaxDuration <= 0 || b.Epsilon <= 0 {
				errs = append(errs, ValidationError{Code: "ADG060", Node: id, Message: "every node in a cycle requires maxIterations, maxCost, maxDuration and epsilon"})
			}
		}
	}

	reach := reachability(def.Nodes)
	for i := 0; i < len(def.Nodes); i++ {
		for j := i + 1; j < len(def.Nodes); j++ {
			a, b := def.Nodes[i], def.Nodes[j]
			if reach[a.ID][b.ID] || reach[b.ID][a.ID] {
				continue
			}
			if overlap(a.Writes, b.Writes) && !overlap(a.ResourceKeys, b.ResourceKeys) {
				errs = append(errs, ValidationError{Code: "ADG070", Node: a.ID + "," + b.ID, Message: "potential parallel writers share data but no common resource key"})
			}
		}
	}
	return errs
}

func cloneNode(n Node) Node {
	n.DependsOn = append([]string(nil), n.DependsOn...)
	n.Next = append([]Transition(nil), n.Next...)
	n.Requires = append([]string(nil), n.Requires...)
	n.Produces = append([]string(nil), n.Produces...)
	n.Writes = append([]string(nil), n.Writes...)
	n.ResourceKeys = append([]string(nil), n.ResourceKeys...)
	n.RequiredPermissions = append([]string(nil), n.RequiredPermissions...)
	if n.Gate != nil {
		g := *n.Gate
		g.HardFloors = cloneFloatMap(g.HardFloors)
		g.RepairFrom = append([]string(nil), g.RepairFrom...)
		n.Gate = &g
	}
	if n.Join != nil {
		j := *n.Join
		n.Join = &j
	}
	if n.Wait != nil {
		w := *n.Wait
		n.Wait = &w
	}
	if n.Human != nil {
		h := *n.Human
		n.Human = &h
	}
	if n.Loop != nil {
		l := *n.Loop
		n.Loop = &l
	}
	return n
}
func setOf(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range values {
		if v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}
func cloneIntMap(in map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneFloatMap(in map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func overlap(a, b []string) bool {
	s := setOf(a)
	for _, v := range b {
		if _, ok := s[v]; ok {
			return true
		}
	}
	return false
}

func reachability(nodes []Node) map[string]map[string]bool {
	adj := map[string][]string{}
	for _, n := range nodes {
		for _, tr := range n.Next {
			adj[n.ID] = append(adj[n.ID], tr.To)
		}
		for _, cand := range nodes {
			for _, dep := range cand.DependsOn {
				if dep == n.ID {
					adj[n.ID] = append(adj[n.ID], cand.ID)
				}
			}
		}
	}
	out := map[string]map[string]bool{}
	for _, n := range nodes {
		out[n.ID] = map[string]bool{}
		q := append([]string(nil), adj[n.ID]...)
		for len(q) > 0 {
			v := q[0]
			q = q[1:]
			if out[n.ID][v] {
				continue
			}
			out[n.ID][v] = true
			q = append(q, adj[v]...)
		}
	}
	return out
}

func computeDescendants(p *Plan) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for id := range p.Nodes {
		out[id] = map[string]struct{}{}
		q := []string{id}
		seen := map[string]bool{id: true}
		for len(q) > 0 {
			cur := q[0]
			q = q[1:]
			for _, tr := range p.outbound[cur] {
				if !seen[tr.To] {
					seen[tr.To] = true
					out[id][tr.To] = struct{}{}
					q = append(q, tr.To)
				}
			}
			for _, cand := range p.Nodes {
				for _, dep := range cand.DependsOn {
					if dep == cur && !seen[cand.ID] {
						seen[cand.ID] = true
						out[id][cand.ID] = struct{}{}
						q = append(q, cand.ID)
					}
				}
			}
		}
	}
	return out
}

func tarjan(nodes []Node) [][]string {
	adj := map[string][]string{}
	for _, n := range nodes {
		for _, tr := range n.Next {
			adj[n.ID] = append(adj[n.ID], tr.To)
		}
		for _, dep := range n.DependsOn {
			adj[dep] = append(adj[dep], n.ID)
		}
	}
	index := 0
	stack := []string{}
	on := map[string]bool{}
	idx := map[string]int{}
	low := map[string]int{}
	for _, n := range nodes {
		idx[n.ID] = -1
	}
	var sccs [][]string
	var strong func(string)
	strong = func(v string) {
		idx[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		on[v] = true
		for _, w := range adj[v] {
			if idx[w] == -1 {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if on[w] && idx[w] < low[v] {
				low[v] = idx[w]
			}
		}
		if low[v] == idx[v] {
			var c []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				c = append(c, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, c)
		}
	}
	for _, n := range nodes {
		if idx[n.ID] == -1 {
			strong(n.ID)
		}
	}
	return sccs
}

func normalizeRetry(p RetryPolicy) RetryPolicy {
	if p.MaxAttempts <= 0 {
		p = DefaultRetryPolicy()
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = time.Second
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.MaxRetryDuration <= 0 {
		p.MaxRetryDuration = 5 * time.Minute
	}
	if p.JitterFraction < 0 {
		p.JitterFraction = 0
	}
	if p.JitterFraction > 1 {
		p.JitterFraction = 1
	}
	return p
}

func validOutcome(o Outcome) bool {
	switch o {
	case "", OutcomeCompleted, OutcomePass, OutcomeRepair, OutcomeFail, OutcomeHuman, OutcomeRejected, OutcomeCanceled:
		return true
	default:
		return false
	}
}
