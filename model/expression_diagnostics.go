package model

import (
	"fmt"

	"github.com/Homiakus/axiom"
)

func (d *Definition) expressionDiagnostics() axiom.Errors {
	if d == nil {
		return nil
	}
	var out axiom.Errors
	add := func(entity string, expression Expr) {
		if expression.err == nil {
			return
		}
		out = append(out, axiom.Error{
			Code:    "AX510",
			Kind:    "model",
			Entity:  entity,
			Message: expression.err.Error(),
			Hint:    "Use a JSON-serializable Go value, or call model.TryLit(value) when you need to handle literal encoding errors immediately.",
		})
	}

	for _, state := range d.states {
		for _, field := range state.fields {
			if field.defaultValue != nil {
				add("state."+state.name+"."+field.name+".default", *field.defaultValue)
			}
		}
	}
	for _, computed := range d.computeds {
		add("computed."+computed.name, computed.expr)
	}
	for _, fact := range d.facts {
		for index, expression := range fact.when {
			add(fmt.Sprintf("fact.%s.when[%d]", fact.name, index), expression)
		}
		for name, expression := range fact.expose {
			add("fact."+fact.name+".expose."+name, expression)
		}
	}
	for _, policy := range d.policies {
		for name, expression := range policy.entries {
			add("policy."+policy.name+"."+name, expression)
		}
	}
	for _, activity := range d.activities {
		for index, expression := range activity.require {
			add(fmt.Sprintf("activity.%s.require[%d]", activity.name, index), expression)
		}
		for name, expression := range activity.input {
			add("activity."+activity.name+".input."+name, expression)
		}
		if activity.idempotency != nil {
			add("activity."+activity.name+".idempotencyKey", *activity.idempotency)
		}
	}
	for _, rule := range d.rules {
		for index, trigger := range rule.triggers {
			if trigger.err != nil {
				out = append(out, axiom.Error{
					Code:    "AX510",
					Kind:    "model",
					Entity:  fmt.Sprintf("rule.%s.trigger[%d]", rule.name, index),
					Message: trigger.err.Error(),
					Hint:    "Use a valid field expression when constructing changed(...) triggers.",
				})
			}
		}
		for index, expression := range rule.when {
			add(fmt.Sprintf("rule.%s.when[%d]", rule.name, index), expression)
		}
		for index, expression := range rule.require {
			add(fmt.Sprintf("rule.%s.require[%d]", rule.name, index), expression)
		}
		for name, expression := range rule.writes {
			add("rule."+rule.name+".write."+name, expression)
		}
	}
	for _, claim := range d.claims {
		for index, expression := range claim.expressions {
			add(fmt.Sprintf("claim.%s[%d]", claim.name, index), expression)
		}
	}
	for _, query := range d.queries {
		for name, expression := range query.values {
			add("query."+query.name+"."+name, expression)
		}
	}
	return out
}
