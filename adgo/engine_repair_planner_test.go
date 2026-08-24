package adgo

import "testing"

type engineRepairPlannerProbe struct{}

func (*engineRepairPlannerProbe) PlanRepair(*Plan, *Execution, string, []Violation) (RepairPlan, error) {
	return RepairPlan{}, nil
}

func TestWithEngineRepairPlannerInstallsRuntimePlanner(t *testing.T) {
	plan, err := Compile(Definition{
		ID: "engine-repair-option",
		Version: "1",
		Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "Work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	planner := &engineRepairPlannerProbe{}
	engine, err := NewEngine(plan, NewMemoryStore(), NewRegistry(), WithEngineRepairPlanner(planner))
	if err != nil {
		t.Fatal(err)
	}
	if engine.runtime.repair != planner {
		t.Fatal("custom repair planner was not propagated to Engine runtime")
	}
}
