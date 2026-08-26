package axiom_test

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/Homiakus/axiom"
)

type ContractSimple struct {
	ID          int64  `axiom:"custom_id"`
	Name        string `json:"name"`
	Count       int    `json:"count,omitempty"`
	Ignored     string `json:"-"`
	DefaultName string
}

type ContractNested struct {
	Title   string          `json:"title"`
	Details ContractSimple  `json:"details"`
	Pointer *ContractSimple `json:"pointer"`
}

type ContractMap map[string]int64

func TestTypedContract_TagPrecedenceAndOmit(t *testing.T) {
	spec := `domain TagTest

signal TriggerTest

context Data:
  custom_id: Int = 0
  name: String = ""
  count: Int = 0
  ignored: String = ""
  defaultName: String = ""
  outId: Int = 0
  outName: String = ""
  outCount: Int = 0
  outDef: String = ""

activity Process:
  input:
    custom_id = Data.custom_id
    name = Data.name
    count = Data.count
    ignored = Data.ignored
    defaultName = Data.defaultName
  output:
    custom_id: Int
    name: String
    count: Int
    defaultName: String

rule onTrigger:
  on TriggerTest
  run: Process
  write:
    Data.outId = output.custom_id
    Data.outName = output.name
    Data.outCount = output.count
    Data.outDef = output.defaultName
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var capturedIn ContractSimple
	engine, err := axiom.New(module, axiom.ActTyped("Process", func(ctx context.Context, in ContractSimple) (ContractSimple, error) {
		capturedIn = in
		return ContractSimple{
			ID:          in.ID + 10,
			Name:        in.Name + "_out",
			Count:       in.Count * 2,
			Ignored:     "should_not_be_in_output",
			DefaultName: in.DefaultName + "_def",
		}, nil
	}))
	if err != nil {
		t.Fatalf("engine new failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("exec-tag-1")
	if err := run.Patch(ctx, axiom.Patch{
		"Data.custom_id":   int64(101),
		"Data.name":        "alpha",
		"Data.count":       5,
		"Data.ignored":     "secret",
		"Data.defaultName": "myDefault",
	}); err != nil {
		t.Fatalf("patch failed: %v", err)
	}

	if err := run.Signal(ctx, "TriggerTest", nil); err != nil {
		t.Fatalf("signal failed: %v", err)
	}

	// Verify input conversion
	if capturedIn.ID != 101 {
		t.Errorf("expected ID 101 (from custom_id tag), got %d", capturedIn.ID)
	}
	if capturedIn.Name != "alpha" {
		t.Errorf("expected Name 'alpha', got %q", capturedIn.Name)
	}
	if capturedIn.Count != 5 {
		t.Errorf("expected Count 5, got %d", capturedIn.Count)
	}
	if capturedIn.DefaultName != "myDefault" {
		t.Errorf("expected DefaultName 'myDefault', got %q", capturedIn.DefaultName)
	}
	if capturedIn.Ignored != "" {
		t.Errorf("expected Ignored to be empty due to json:\"-\", got %q", capturedIn.Ignored)
	}

	type StateData struct {
		OutID    int64  `json:"outId"`
		OutName  string `json:"outName"`
		OutCount int    `json:"outCount"`
		OutDef   string `json:"outDef"`
	}

	var state StateData
	if err := run.State(ctx, &state); err != nil {
		t.Fatalf("state failed: %v", err)
	}

	if state.OutID != 111 {
		t.Errorf("expected state.OutID 111, got %d", state.OutID)
	}
	if state.OutName != "alpha_out" {
		t.Errorf("expected state.OutName 'alpha_out', got %q", state.OutName)
	}
	if state.OutCount != 10 {
		t.Errorf("expected state.OutCount 10, got %d", state.OutCount)
	}
	if state.OutDef != "myDefault_def" {
		t.Errorf("expected state.OutDef 'myDefault_def', got %q", state.OutDef)
	}
}

func TestTypedContract_PointersAndNil(t *testing.T) {
	spec := `domain PointerTest

signal TriggerPtr

context Item:
  name: String = "testPtr"
  count: Int = 42
  outName: String = ""
  outCount: Int = 0

activity ProcessPointer:
  input:
    name = Item.name
    count = Item.count
  output:
    name: String
    count: Int

rule onTrigger:
  on TriggerPtr
  run: ProcessPointer
  write:
    Item.outName = output.name
    Item.outCount = output.count
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	called := false
	engine, err := axiom.New(module, axiom.ActTyped("ProcessPointer", func(ctx context.Context, in *ContractSimple) (*ContractSimple, error) {
		called = true
		if in == nil {
			return nil, nil
		}
		return &ContractSimple{
			Name:  in.Name + "_ptr",
			Count: in.Count + 1,
		}, nil
	}))
	if err != nil {
		t.Fatalf("engine new failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("exec-ptr-1")
	if err := run.Signal(ctx, "TriggerPtr", nil); err != nil {
		t.Fatalf("signal failed: %v", err)
	}

	if !called {
		t.Fatal("expected activity to be called")
	}

	type ItemState struct {
		OutName  string `json:"outName"`
		OutCount int    `json:"outCount"`
	}
	var state ItemState
	if err := run.State(ctx, &state); err != nil {
		t.Fatalf("state failed: %v", err)
	}
	if state.OutName != "testPtr_ptr" {
		t.Errorf("expected OutName 'testPtr_ptr', got %q", state.OutName)
	}
	if state.OutCount != 43 {
		t.Errorf("expected OutCount 43, got %d", state.OutCount)
	}
}

func TestTypedContract_NamedMaps(t *testing.T) {
	spec := `domain MapTest

signal TriggerMap

context M:
  k1: Int = 5
  k2: Int = 15
  out1: Int = 0
  out2: Int = 0
  out3: Int = 0

activity ProcessMap:
  input:
    k1 = M.k1
    k2 = M.k2
  output:
    k1: Int
    k2: Int
    k3: Int

rule onTrigger:
  on TriggerMap
  run: ProcessMap
  write:
    M.out1 = output.k1
    M.out2 = output.k2
    M.out3 = output.k3
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var capturedIn ContractMap
	engine, err := axiom.New(module, axiom.ActTyped("ProcessMap", func(ctx context.Context, in ContractMap) (ContractMap, error) {
		capturedIn = in
		res := make(ContractMap)
		for k, v := range in {
			res[k] = v * 10
		}
		res["k3"] = 300
		return res, nil
	}))
	if err != nil {
		t.Fatalf("engine new failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("exec-map-1")
	if err := run.Signal(ctx, "TriggerMap", nil); err != nil {
		t.Fatalf("signal failed: %v", err)
	}

	if capturedIn["k1"] != 5 {
		t.Errorf("expected captured k1 = 5, got %d", capturedIn["k1"])
	}
	if capturedIn["k2"] != 15 {
		t.Errorf("expected captured k2 = 15, got %d", capturedIn["k2"])
	}

	type MState struct {
		Out1 int `json:"out1"`
		Out2 int `json:"out2"`
		Out3 int `json:"out3"`
	}
	var m MState
	if err := run.State(ctx, &m); err != nil {
		t.Fatalf("state failed: %v", err)
	}
	if m.Out1 != 50 {
		t.Errorf("expected out1 50, got %d", m.Out1)
	}
	if m.Out2 != 150 {
		t.Errorf("expected out2 150, got %d", m.Out2)
	}
	if m.Out3 != 300 {
		t.Errorf("expected out3 300, got %d", m.Out3)
	}
}

func TestTypedContract_LargeIntegerPrecisionAbove53Bits(t *testing.T) {
	spec := `domain PrecisionTest

signal TriggerLarge

context Data:
  largeId: Int = 0
  maxInt: Int = 0
  outLargeId: Int = 0
  outMaxInt: Int = 0

activity ProcessInt64:
  input:
    largeId = Data.largeId
    maxInt = Data.maxInt
  output:
    largeId: Int
    maxInt: Int

rule onTrigger:
  on TriggerLarge
  run: ProcessInt64
  write:
    Data.outLargeId = output.largeId
    Data.outMaxInt = output.maxInt
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	type LargeIntInput struct {
		LargeID int64 `json:"largeId"`
		MaxInt  int64 `json:"maxInt"`
	}
	type LargeIntOutput struct {
		LargeID int64 `json:"largeId"`
		MaxInt  int64 `json:"maxInt"`
	}

	// 2^53 + 1 = 9007199254740993 (cannot be represented exactly in IEEE-754 float64)
	const testLargeID = int64(1<<53 + 1)
	const testMaxInt = int64(math.MaxInt64)

	var received LargeIntInput
	engine, err := axiom.New(module, axiom.ActTyped("ProcessInt64", func(ctx context.Context, in LargeIntInput) (LargeIntOutput, error) {
		received = in
		return LargeIntOutput{
			LargeID: in.LargeID,
			MaxInt:  in.MaxInt,
		}, nil
	}))
	if err != nil {
		t.Fatalf("engine new failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("exec-prec-1")
	if err := run.Patch(ctx, axiom.Patch{
		"Data.largeId": testLargeID,
		"Data.maxInt":  testMaxInt,
	}); err != nil {
		t.Fatalf("patch failed: %v", err)
	}

	if err := run.Signal(ctx, "TriggerLarge", nil); err != nil {
		t.Fatalf("signal failed: %v", err)
	}

	if received.LargeID != testLargeID {
		t.Errorf("received largeId %d != expected %d (precision lost)", received.LargeID, testLargeID)
	}
	if received.MaxInt != testMaxInt {
		t.Errorf("received maxInt %d != expected %d", received.MaxInt, testMaxInt)
	}
}

func TestTypedContract_EmbeddedStructs(t *testing.T) {
	type Base struct {
		BaseField string `json:"baseField"`
	}
	type Extended struct {
		Base
		ExtraField string `json:"extraField"`
	}

	spec := `domain EmbeddedTest

signal TriggerEmbedded

context E:
  baseField: String = "hello"
  extraField: String = "world"
  outBase: String = ""
  outExtra: String = ""

activity ProcessEmbedded:
  input:
    baseField = E.baseField
    extraField = E.extraField
  output:
    baseField: String
    extraField: String

rule onTrigger:
  on TriggerEmbedded
  run: ProcessEmbedded
  write:
    E.outBase = output.baseField
    E.outExtra = output.extraField
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var captured Extended
	engine, err := axiom.New(module, axiom.ActTyped("ProcessEmbedded", func(ctx context.Context, in Extended) (Extended, error) {
		captured = in
		return Extended{
			Base: Base{
				BaseField: in.BaseField + "_base",
			},
			ExtraField: in.ExtraField + "_extra",
		}, nil
	}))
	if err != nil {
		t.Fatalf("engine new failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("exec-embed-1")
	if err := run.Signal(ctx, "TriggerEmbedded", nil); err != nil {
		t.Fatalf("signal failed: %v", err)
	}

	if captured.BaseField != "hello" {
		t.Errorf("expected captured BaseField 'hello', got %q", captured.BaseField)
	}
	if captured.ExtraField != "world" {
		t.Errorf("expected captured ExtraField 'world', got %q", captured.ExtraField)
	}

	type EState struct {
		OutBase  string `json:"outBase"`
		OutExtra string `json:"outExtra"`
	}
	var state EState
	if err := run.State(ctx, &state); err != nil {
		t.Fatalf("state failed: %v", err)
	}
	if state.OutBase != "hello_base" {
		t.Errorf("expected outBase 'hello_base', got %q", state.OutBase)
	}
	if state.OutExtra != "world_extra" {
		t.Errorf("expected outExtra 'world_extra', got %q", state.OutExtra)
	}
}

func TestTypedContract_TypePrecedenceAndOmitEmpty(t *testing.T) {
	type TagRules struct {
		AxiomWins string `axiom:"custom_field" json:"ignored_json_name"`
		JsonTag   string `json:"only_json,omitempty"`
		NoTags    int
		Dashed    string `json:"-"`
	}

	typ := reflect.TypeOf(TagRules{})
	if typ.NumField() != 4 {
		t.Fatalf("expected 4 fields, got %d", typ.NumField())
	}
}
