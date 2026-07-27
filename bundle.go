package axiom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

const DSLVersion = "axm/v1"

type ModuleBundle struct {
	Module        *Module
	SourceHash    string
	CompiledHash  string
	DSLVersion    string
	Activities    []string
	ContextFields []string
	Rules         []string
	Claims        []string
}

type BundleDiff struct {
	AddedActivities   []string
	RemovedActivities []string
	AddedFields       []string
	RemovedFields     []string
	AddedRules        []string
	RemovedRules      []string
	AddedClaims       []string
	RemovedClaims     []string
}

type ImpactReport struct {
	Fields     []string
	Rules      []string
	Activities []string
	Claims     []string
	Queries    []string
}

func CompileBundle(source []byte, opts ...CompileOption) (*ModuleBundle, error) {
	module, err := Compile(source, opts...)
	if err != nil {
		return nil, err
	}
	bundle := &ModuleBundle{
		Module:        module,
		SourceHash:    module.SourceHash,
		DSLVersion:    module.DSLVersion,
		Activities:    sortedKeys(module.Activities),
		ContextFields: contextFields(module),
		Rules:         sortedKeys(module.Rules),
		Claims:        sortedKeys(module.Claims),
	}
	if bundle.SourceHash == "" {
		bundle.SourceHash = hashBytes(source)
	}
	bundle.CompiledHash = module.CompiledHash
	if bundle.CompiledHash == "" {
		bundle.CompiledHash = hashJSON(map[string]any{
			"dsl":        bundle.DSLVersion,
			"activities": bundle.Activities,
			"fields":     bundle.ContextFields,
			"rules":      bundle.Rules,
			"claims":     bundle.Claims,
		})
	}
	return bundle, nil
}

func (b *ModuleBundle) Diff(other *ModuleBundle) BundleDiff {
	if b == nil || other == nil {
		return BundleDiff{}
	}
	return BundleDiff{
		AddedActivities:   difference(other.Activities, b.Activities),
		RemovedActivities: difference(b.Activities, other.Activities),
		AddedFields:       difference(other.ContextFields, b.ContextFields),
		RemovedFields:     difference(b.ContextFields, other.ContextFields),
		AddedRules:        difference(other.Rules, b.Rules),
		RemovedRules:      difference(b.Rules, other.Rules),
		AddedClaims:       difference(other.Claims, b.Claims),
		RemovedClaims:     difference(b.Claims, other.Claims),
	}
}

func (b *ModuleBundle) Impact(changeSet []string) ImpactReport {
	report := ImpactReport{Fields: uniqueSorted(changeSet)}
	if b == nil || b.Module == nil {
		return report
	}
	rules := map[string]struct{}{}
	activities := map[string]struct{}{}
	claims := map[string]struct{}{}
	queries := map[string]struct{}{}
	for _, field := range changeSet {
		for _, node := range b.Module.Indexes.ContextFieldIndex[field] {
			switch node.Kind {
			case "rule":
				rules[node.Name] = struct{}{}
				if rule, ok := b.Module.Rules[node.Name]; ok && rule.Run != "" {
					activities[rule.Run] = struct{}{}
				}
			case "claim":
				claims[node.Name] = struct{}{}
			case "query":
				queries[node.Name] = struct{}{}
			}
		}
		for _, rule := range b.Module.Indexes.ChangedIndex[field] {
			rules[rule] = struct{}{}
			if decl, ok := b.Module.Rules[rule]; ok && decl.Run != "" {
				activities[decl.Run] = struct{}{}
			}
		}
		for _, claim := range b.Module.Indexes.ClaimIndex[field] {
			claims[claim] = struct{}{}
		}
	}
	report.Rules = setToSorted(rules)
	report.Activities = setToSorted(activities)
	report.Claims = setToSorted(claims)
	report.Queries = setToSorted(queries)
	return report
}

func (b *ModuleBundle) ValidateCompatibility(previous *ModuleBundle) error {
	if b == nil || previous == nil {
		return nil
	}
	diff := previous.Diff(b)
	var errs Errors
	for _, field := range diff.RemovedFields {
		errs = append(errs, Error{Code: "AX801", Kind: "compat", Entity: field, Message: "context field removed from module bundle"})
	}
	for _, activity := range diff.RemovedActivities {
		errs = append(errs, Error{Code: "AX802", Kind: "compat", Entity: activity, Message: "activity removed from module bundle"})
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hashJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return hashBytes(data)
}

func contextFields(module *Module) []string {
	if module == nil || module.AST == nil {
		return nil
	}
	var out []string
	for _, contextDecl := range module.AST.Contexts {
		for _, field := range contextDecl.Fields {
			out = append(out, contextDecl.Name+"."+field.Name)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func difference(left []string, right []string) []string {
	rightSet := map[string]struct{}{}
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	var out []string
	for _, value := range left {
		if _, ok := rightSet[value]; !ok {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	return setToSorted(set)
}

func setToSorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
