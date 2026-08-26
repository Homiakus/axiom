package axiom_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/internal/typedconv"
)

// Small payload
type SmallInput struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type SmallOutput struct {
	ID      int64  `json:"id"`
	Success bool   `json:"success"`
}

// Medium payload
type MediumInput struct {
	ID        int64          `json:"id"`
	Title     string         `json:"title"`
	Count     int            `json:"count"`
	Tags      []string       `json:"tags"`
	Metadata  map[string]int `json:"metadata"`
	IsActive  bool           `json:"isActive"`
	Score     float64        `json:"score"`
}

type MediumOutput struct {
	ID        int64          `json:"id"`
	Status    string         `json:"status"`
	Count     int            `json:"count"`
	TagsCount int            `json:"tagsCount"`
	Processed bool           `json:"processed"`
}

// Deeply nested payload
type InnerDetails struct {
	Code        string `json:"code"`
	SubCount    int    `json:"subCount"`
	Description string `json:"description"`
}

type NestedSection struct {
	SectionID string       `json:"sectionId"`
	Details   InnerDetails `json:"details"`
}

type DeepInput struct {
	RootID   string          `json:"rootId"`
	Level1   NestedSection   `json:"level1"`
	Pointer  *NestedSection  `json:"pointer"`
	Values   []int           `json:"values"`
}

type DeepOutput struct {
	RootID    string `json:"rootId"`
	CodeEcho  string `json:"codeEcho"`
	SumValues int    `json:"sumValues"`
}

func setupBenchmarkEngine(b *testing.B, spec string, option axiom.Option) *axiom.Engine {
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}
	engine, err := axiom.New(module, option)
	if err != nil {
		b.Fatalf("axiom.New failed: %v", err)
	}
	return engine
}

func BenchmarkAct_Small(b *testing.B) {
	spec := `domain BenchSmall
signal Trigger
context S:
  id: Int = 100
  name: String = "alpha"
  outId: Int = 0
  outSuccess: Bool = false
activity ProcessSmall:
  input:
    id = S.id
    name = S.name
  output:
    id: Int
    success: Bool
rule onTrigger:
  on Trigger
  run: ProcessSmall
  write:
    S.outId = output.id
    S.outSuccess = output.success
`
	engine := setupBenchmarkEngine(b, spec, axiom.Act("ProcessSmall", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
		id, _ := input["id"].(int64)
		if id == 0 {
			if idInt, ok := input["id"].(int); ok {
				id = int64(idInt)
			}
		}
		return axiom.Output{
			"id":      id + 1,
			"success": true,
		}, nil
	}))

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		execID := fmt.Sprintf("bench-act-small-%d", i)
		run := engine.Execution(execID)
		if err := run.Signal(ctx, "Trigger", nil); err != nil {
			b.Fatalf("signal failed: %v", err)
		}
	}
}

func BenchmarkActTyped_Small(b *testing.B) {
	spec := `domain BenchSmall
signal Trigger
context S:
  id: Int = 100
  name: String = "alpha"
  outId: Int = 0
  outSuccess: Bool = false
activity ProcessSmall:
  input:
    id = S.id
    name = S.name
  output:
    id: Int
    success: Bool
rule onTrigger:
  on Trigger
  run: ProcessSmall
  write:
    S.outId = output.id
    S.outSuccess = output.success
`
	engine := setupBenchmarkEngine(b, spec, axiom.ActTyped("ProcessSmall", func(ctx context.Context, in SmallInput) (SmallOutput, error) {
		return SmallOutput{
			ID:      in.ID + 1,
			Success: true,
		}, nil
	}))

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		execID := fmt.Sprintf("bench-typed-small-%d", i)
		run := engine.Execution(execID)
		if err := run.Signal(ctx, "Trigger", nil); err != nil {
			b.Fatalf("signal failed: %v", err)
		}
	}
}

func BenchmarkAct_Medium(b *testing.B) {
	spec := `domain BenchMedium
signal Trigger
context M:
  id: Int = 500
  title: String = "Medium Payload"
  count: Int = 42
  outId: Int = 0
  outStatus: String = ""
  outCount: Int = 0
  outTagsCount: Int = 0
  outProcessed: Bool = false
activity ProcessMedium:
  input:
    id = M.id
    title = M.title
    count = M.count
  output:
    id: Int
    status: String
    count: Int
    tagsCount: Int
    processed: Bool
rule onTrigger:
  on Trigger
  run: ProcessMedium
  write:
    M.outId = output.id
    M.outStatus = output.status
    M.outCount = output.count
    M.outTagsCount = output.tagsCount
    M.outProcessed = output.processed
`
	engine := setupBenchmarkEngine(b, spec, axiom.Act("ProcessMedium", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
		id, _ := input["id"].(int64)
		if id == 0 {
			if idInt, ok := input["id"].(int); ok {
				id = int64(idInt)
			}
		}
		count, _ := input["count"].(int)
		return axiom.Output{
			"id":        id + 1,
			"status":    "completed",
			"count":     count * 2,
			"tagsCount": 3,
			"processed": true,
		}, nil
	}))

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		execID := fmt.Sprintf("bench-act-med-%d", i)
		run := engine.Execution(execID)
		if err := run.Signal(ctx, "Trigger", nil); err != nil {
			b.Fatalf("signal failed: %v", err)
		}
	}
}

func BenchmarkActTyped_Medium(b *testing.B) {
	spec := `domain BenchMedium
signal Trigger
context M:
  id: Int = 500
  title: String = "Medium Payload"
  count: Int = 42
  outId: Int = 0
  outStatus: String = ""
  outCount: Int = 0
  outTagsCount: Int = 0
  outProcessed: Bool = false
activity ProcessMedium:
  input:
    id = M.id
    title = M.title
    count = M.count
  output:
    id: Int
    status: String
    count: Int
    tagsCount: Int
    processed: Bool
rule onTrigger:
  on Trigger
  run: ProcessMedium
  write:
    M.outId = output.id
    M.outStatus = output.status
    M.outCount = output.count
    M.outTagsCount = output.tagsCount
    M.outProcessed = output.processed
`
	engine := setupBenchmarkEngine(b, spec, axiom.ActTyped("ProcessMedium", func(ctx context.Context, in MediumInput) (MediumOutput, error) {
		return MediumOutput{
			ID:        in.ID + 1,
			Status:    "completed",
			Count:     in.Count * 2,
			TagsCount: len(in.Tags),
			Processed: true,
		}, nil
	}))

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		execID := fmt.Sprintf("bench-typed-med-%d", i)
		run := engine.Execution(execID)
		if err := run.Signal(ctx, "Trigger", nil); err != nil {
			b.Fatalf("signal failed: %v", err)
		}
	}
}

func BenchmarkActTyped_DirectConversionMicro(b *testing.B) {
	convIn, err := typedconv.CompileInput[MediumInput]()
	if err != nil {
		b.Fatalf("compileInput err: %v", err)
	}
	convOut, err := typedconv.CompileOutput[MediumOutput]()
	if err != nil {
		b.Fatalf("compileOutput err: %v", err)
	}

	rawInput := map[string]any{
		"id":       int64(12345),
		"title":    "Direct Micro Benchmark",
		"count":    100,
		"tags":     []any{"tag1", "tag2", "tag3"},
		"metadata": map[string]any{"a": 1, "b": 2},
		"isActive": true,
		"score":    99.9,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		in, err := convIn(rawInput)
		if err != nil {
			b.Fatal(err)
		}
		out := MediumOutput{
			ID:        in.ID,
			Status:    "ok",
			Count:     in.Count,
			TagsCount: len(in.Tags),
			Processed: true,
		}
		res, err := convOut(out)
		if err != nil || res == nil {
			b.Fatal(err)
		}
	}
}
