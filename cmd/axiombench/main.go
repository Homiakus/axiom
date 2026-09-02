package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/adgo"
)

const counterSource = `domain CounterBench

signal Increment:
  by: Int

context Counter:
  value: Int = 0

rule increment:
  on Increment
  write:
    Counter.value = Counter.value + signal.by
`

type counterState struct {
	Value int `json:"value"`
}

type increment struct {
	By int `json:"by"`
}

func (increment) AxiomEventName() string { return "Increment" }

type latencySummary struct {
	MeanUS float64 `json:"mean_us"`
	P50US  float64 `json:"p50_us"`
	P95US  float64 `json:"p95_us"`
	P99US  float64 `json:"p99_us"`
	MaxUS  float64 `json:"max_us"`
}

type scenarioResult struct {
	Scenario      string         `json:"scenario"`
	Operations    int            `json:"operations"`
	Concurrency   int            `json:"concurrency"`
	DurationMS    float64        `json:"duration_ms"`
	ThroughputOPS float64        `json:"throughput_ops"`
	Errors        int64          `json:"errors"`
	InvariantOK   bool           `json:"invariant_ok"`
	Expected      int            `json:"expected,omitempty"`
	Actual        int            `json:"actual,omitempty"`
	Latency       latencySummary `json:"latency"`
	Notes         string         `json:"notes,omitempty"`
}

type report struct {
	GeneratedAt string           `json:"generated_at"`
	GoVersion   string           `json:"go_version"`
	GOOS        string           `json:"goos"`
	GOARCH      string           `json:"goarch"`
	CPUs        int              `json:"cpus"`
	Results     []scenarioResult `json:"results"`
}

type comparisonRow struct {
	Scenario          string
	BaselineThroughput float64
	CurrentThroughput  float64
	ThroughputDeltaPct float64
	BaselineP99US      float64
	CurrentP99US       float64
	P99DeltaPct        float64
	Regression         bool
}

type runMetrics struct {
	latencies []int64
	errors    int64
	duration  time.Duration
}

func main() {
	var (
		memoryOps      = flag.Int("memory-ops", 20000, "operations per memory scenario")
		pebbleOps      = flag.Int("pebble-ops", 1000, "operations per Pebble scenario")
		replayOps      = flag.Int("replay-events", 1000, "events in replay history")
		replayRuns     = flag.Int("replay-runs", 200, "replay samples")
		workers        = flag.Int("concurrency", 8, "parallel workers")
		jsonPath       = flag.String("json", "benchmark-results.json", "JSON result path")
		mdPath         = flag.String("markdown", "benchmark-results.md", "Markdown result path")
		baselinePath   = flag.String("compare-baseline", "", "path to baseline JSON report for regression comparison")
		maxRegressPct  = flag.Float64("max-regression-pct", 30.0, "maximum allowed regression percentage in strict mode")
		strict         = flag.Bool("strict", true, "exit non-zero on errors or invariant failure")
	)
	flag.Parse()

	if *workers < 1 {
		*workers = 1
	}
	ctx := context.Background()
	results := make([]scenarioResult, 0, 10)

	results = append(results, runFlowDistinct(ctx, *memoryOps, *workers))
	results = append(results, runFlowContended(ctx, *memoryOps, *workers))
	results = append(results, runRuntimeDistinct(ctx, *memoryOps, *workers))
	results = append(results, runRuntimeContended(ctx, *memoryOps, *workers))
	results = append(results, runRuntimeCold(ctx, max(1000, *memoryOps/4), *workers))
	results = append(results, runPebbleCold(ctx, *pebbleOps, *workers, true))
	results = append(results, runPebbleCold(ctx, max(100, *pebbleOps/4), *workers, false))
	results = append(results, runPebbleReopen(ctx, max(100, *pebbleOps/5)))
	results = append(results, runReplay(ctx, *replayOps, *replayRuns))
	results = append(results, runADGOWorkflow(ctx, max(500, *memoryOps/10), *workers))

	rep := report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GoVersion:   runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		Results:     results,
	}
	if err := writeReport(*jsonPath, *mdPath, rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Print(renderMarkdown(rep))

	hasRegression := false
	if *baselinePath != "" {
		baselineRep, err := loadReport(*baselinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load baseline report: %v\n", err)
			os.Exit(2)
		}
		comparisons := compareReports(baselineRep, rep, *maxRegressPct)
		fmt.Print(renderComparisonMarkdown(comparisons, *maxRegressPct))
		for _, row := range comparisons {
			if row.Regression {
				hasRegression = true
			}
		}
	}

	if *strict {
		for _, result := range results {
			if result.Errors != 0 || !result.InvariantOK {
				os.Exit(1)
			}
		}
		if hasRegression {
			fmt.Fprintln(os.Stderr, "FAIL: benchmark regression exceeded threshold")
			os.Exit(1)
		}
	}
}

func newFlow(name string) *axiom.Flow[counterState] {
	flow := axiom.NewFlow(name, counterState{})
	axiom.Handle(flow, func(_ context.Context, state counterState, event increment) (axiom.FlowResult[counterState], error) {
		state.Value += event.By
		return axiom.Next(state), nil
	})
	return flow
}

func runFlowDistinct(ctx context.Context, operations, workers int) scenarioResult {
	engine, err := axiom.OpenFlow(newFlow("bench-flow-distinct"))
	if err != nil {
		return failed("flow_memory_distinct", operations, workers, err)
	}
	metrics := runParallel(operations, workers, func(worker, _ int) error {
		return engine.Execution(fmt.Sprintf("worker-%d", worker)).Dispatch(ctx, increment{By: 1})
	})
	actual := 0
	for worker := 0; worker < workers; worker++ {
		state, stateErr := engine.Execution(fmt.Sprintf("worker-%d", worker)).State(ctx)
		if stateErr != nil {
			metrics.errors++
			continue
		}
		actual += state.Value
	}
	return finish("flow_memory_distinct", operations, workers, metrics, operations, actual, "one execution per worker")
}

func runFlowContended(ctx context.Context, operations, workers int) scenarioResult {
	engine, err := axiom.OpenFlow(newFlow("bench-flow-contended"))
	if err != nil {
		return failed("flow_memory_same_execution", operations, workers, err)
	}
	run := engine.Execution("shared")
	metrics := runParallel(operations, workers, func(_, _ int) error {
		return run.Dispatch(ctx, increment{By: 1})
	})
	state, stateErr := run.State(ctx)
	if stateErr != nil {
		metrics.errors++
	}
	return finish("flow_memory_same_execution", operations, workers, metrics, operations, state.Value, "all workers update one execution")
}

func compileCounter() (*axiom.Module, error) {
	return axiom.Compile([]byte(counterSource), axiom.WithSourceName("benchmark.axm"))
}

func runRuntimeDistinct(ctx context.Context, operations, workers int) scenarioResult {
	module, err := compileCounter()
	if err != nil {
		return failed("runtime_memory_distinct", operations, workers, err)
	}
	engine, err := axiom.New(module, axiom.WithTraceLevel(axiom.TraceMinimal))
	if err != nil {
		return failed("runtime_memory_distinct", operations, workers, err)
	}
	for worker := 0; worker < workers; worker++ {
		if err := engine.Start(ctx, fmt.Sprintf("worker-%d", worker), nil); err != nil {
			return failed("runtime_memory_distinct", operations, workers, err)
		}
	}
	metrics := runParallel(operations, workers, func(worker, _ int) error {
		return engine.Execution(fmt.Sprintf("worker-%d", worker)).Dispatch(ctx, increment{By: 1})
	})
	actual := 0
	for worker := 0; worker < workers; worker++ {
		var state counterState
		if err := engine.Execution(fmt.Sprintf("worker-%d", worker)).State(ctx, &state); err != nil {
			metrics.errors++
			continue
		}
		actual += state.Value
	}
	return finish("runtime_memory_distinct", operations, workers, metrics, operations, actual, "compiled Plan, one execution per worker")
}

func runRuntimeContended(ctx context.Context, operations, workers int) scenarioResult {
	module, err := compileCounter()
	if err != nil {
		return failed("runtime_memory_same_execution", operations, workers, err)
	}
	engine, err := axiom.New(module, axiom.WithTraceLevel(axiom.TraceMinimal))
	if err != nil {
		return failed("runtime_memory_same_execution", operations, workers, err)
	}
	if err := engine.Start(ctx, "shared", nil); err != nil {
		return failed("runtime_memory_same_execution", operations, workers, err)
	}
	run := engine.Execution("shared")
	metrics := runParallel(operations, workers, func(_, _ int) error {
		return run.Dispatch(ctx, increment{By: 1})
	})
	var state counterState
	if err := run.State(ctx, &state); err != nil {
		metrics.errors++
	}
	return finish("runtime_memory_same_execution", operations, workers, metrics, operations, state.Value, "all workers update one compiled execution")
}

func runRuntimeCold(ctx context.Context, operations, workers int) scenarioResult {
	module, err := compileCounter()
	if err != nil {
		return failed("runtime_memory_cold", operations, workers, err)
	}
	engine, err := axiom.New(module, axiom.WithTraceLevel(axiom.TraceMinimal))
	if err != nil {
		return failed("runtime_memory_cold", operations, workers, err)
	}
	metrics := runParallel(operations, workers, func(worker, sequence int) error {
		id := fmt.Sprintf("cold-%d-%d", worker, sequence)
		return engine.Execution(id).Dispatch(ctx, increment{By: 1})
	})
	return finish("runtime_memory_cold", operations, workers, metrics, 0, 0, "new execution for every operation")
}

func runPebbleCold(ctx context.Context, operations, workers int, noSync bool) scenarioResult {
	name := "runtime_pebble_sync_cold"
	var options []axiom.PebbleOption
	if noSync {
		name = "runtime_pebble_nosync_cold"
		options = append(options, axiom.PebbleNoSync())
	}
	dir, err := os.MkdirTemp("", "axiom-bench-pebble-")
	if err != nil {
		return failed(name, operations, workers, err)
	}
	defer os.RemoveAll(dir)
	store, err := axiom.OpenPebble(dir, options...)
	if err != nil {
		return failed(name, operations, workers, err)
	}
	defer store.Close()
	module, err := compileCounter()
	if err != nil {
		return failed(name, operations, workers, err)
	}
	engine, err := axiom.New(module, axiom.WithStore(store), axiom.WithTraceLevel(axiom.TraceMinimal))
	if err != nil {
		return failed(name, operations, workers, err)
	}
	metrics := runParallel(operations, workers, func(worker, sequence int) error {
		id := fmt.Sprintf("pebble-%d-%d", worker, sequence)
		return engine.Execution(id).Dispatch(ctx, increment{By: 1})
	})
	return finish(name, operations, workers, metrics, 0, 0, "new durable execution for every operation")
}

func runPebbleReopen(ctx context.Context, reopens int) scenarioResult {
	name := "pebble_reopen_durable"
	dir, err := os.MkdirTemp("", "axiom-bench-reopen-")
	if err != nil {
		return failed(name, reopens, 1, err)
	}
	defer os.RemoveAll(dir)

	module, err := compileCounter()
	if err != nil {
		return failed(name, reopens, 1, err)
	}

	metrics := runParallel(reopens, 1, func(_, _ int) error {
		store, openErr := axiom.OpenPebble(dir, axiom.PebbleNoSync())
		if openErr != nil {
			return openErr
		}
		defer store.Close()
		engine, newErr := axiom.New(module, axiom.WithStore(store), axiom.WithTraceLevel(axiom.TraceMinimal))
		if newErr != nil {
			return newErr
		}
		return engine.Execution("reopen-target").Dispatch(ctx, increment{By: 1})
	})

	return finish(name, reopens, 1, metrics, 0, 0, "open, dispatch, and close store per operation")
}

func runReplay(ctx context.Context, events, runs int) scenarioResult {
	module, err := compileCounter()
	if err != nil {
		return failed("replay_history", runs, 1, err)
	}
	store := axiom.NewMemoryStore()
	engine, err := axiom.New(module, axiom.WithStore(store), axiom.WithTraceLevel(axiom.TraceMinimal))
	if err != nil {
		return failed("replay_history", runs, 1, err)
	}
	if err := engine.Start(ctx, "replay", nil); err != nil {
		return failed("replay_history", runs, 1, err)
	}
	for index := 0; index < events; index++ {
		if err := engine.Signal(ctx, "replay", "Increment", axiom.Input{"by": 1}); err != nil {
			return failed("replay_history", runs, 1, err)
		}
	}
	history, err := store.ListHistory(ctx, "replay")
	if err != nil {
		return failed("replay_history", runs, 1, err)
	}
	var actual int
	metrics := runParallel(runs, 1, func(_, _ int) error {
		execution, replayErr := axiom.ReplayFromHistory(module, history)
		if replayErr == nil {
			actual = execution.Context["Counter"]["value"].(int)
		}
		return replayErr
	})
	return finish("replay_history", runs, 1, metrics, events, actual, fmt.Sprintf("replay %d history events", events))
}

func runADGOWorkflow(ctx context.Context, operations, workers int) scenarioResult {
	name := "adgo_memory_workflow"
	plan, err := adgo.Compile(adgo.Definition{
		ID:      "bench-adgo",
		Version: "1",
		Nodes: []adgo.Node{
			{ID: "step1", Kind: adgo.NodeActivity, Activity: "step1", Next: []adgo.Transition{{To: "step2"}}},
			{ID: "step2", Kind: adgo.NodeActivity, Activity: "step2"},
		},
	})
	if err != nil {
		return failed(name, operations, workers, err)
	}

	registry := adgo.NewRegistry()
	registry.Activity("step1", func(context.Context, adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"v1": 1}}, nil
	})
	registry.Activity("step2", func(context.Context, adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"v2": 2}}, nil
	})

	store := adgo.NewMemoryStore()
	engine, err := adgo.NewEngine(plan, store, registry)
	if err != nil {
		return failed(name, operations, workers, err)
	}

	metrics := runParallel(operations, workers, func(worker, sequence int) error {
		id := fmt.Sprintf("adgo-exec-%d-%d", worker, sequence)
		if _, startErr := engine.Start(ctx, id, nil, adgo.BudgetLimit{}); startErr != nil {
			return startErr
		}
		_, runErr := engine.RunLocal(ctx, id, adgo.LocalRunOptions{
			Worker: adgo.WorkerSpec{ID: "bench-worker", Concurrency: 4},
		})
		return runErr
	})

	return finish(name, operations, workers, metrics, 0, 0, "ADGO multi-step workflow execution")
}

func runParallel(operations, workers int, operation func(worker, sequence int) error) runMetrics {
	if operations < 1 {
		operations = 1
	}
	workers = min(max(workers, 1), operations)
	latencies := make([]int64, operations)
	var errorsCount atomic.Int64
	var group sync.WaitGroup
	ready := make(chan struct{})
	offset := 0
	for worker := 0; worker < workers; worker++ {
		count := operations / workers
		if worker < operations%workers {
			count++
		}
		startIndex := offset
		offset += count
		group.Add(1)
		go func(worker, count, startIndex int) {
			defer group.Done()
			<-ready
			for sequence := 0; sequence < count; sequence++ {
				begin := time.Now()
				if err := operation(worker, sequence); err != nil {
					errorsCount.Add(1)
				}
				latencies[startIndex+sequence] = time.Since(begin).Nanoseconds()
			}
		}(worker, count, startIndex)
	}
	started := time.Now()
	close(ready)
	group.Wait()
	return runMetrics{latencies: latencies, errors: errorsCount.Load(), duration: time.Since(started)}
}

func finish(name string, operations, workers int, metrics runMetrics, expected, actual int, notes string) scenarioResult {
	invariantOK := metrics.errors == 0
	if expected != 0 {
		invariantOK = invariantOK && expected == actual
	}
	throughput := 0.0
	if secs := metrics.duration.Seconds(); secs > 0 {
		throughput = float64(operations) / secs
		if math.IsInf(throughput, 0) || math.IsNaN(throughput) {
			throughput = 0.0
		}
	}
	return scenarioResult{
		Scenario:      name,
		Operations:    operations,
		Concurrency:   workers,
		DurationMS:    float64(metrics.duration) / float64(time.Millisecond),
		ThroughputOPS: throughput,
		Errors:        metrics.errors,
		InvariantOK:   invariantOK,
		Expected:      expected,
		Actual:        actual,
		Latency:       summarize(metrics.latencies),
		Notes:         notes,
	}
}

func failed(name string, operations, workers int, err error) scenarioResult {
	return scenarioResult{Scenario: name, Operations: operations, Concurrency: workers, Errors: 1, InvariantOK: false, Notes: err.Error()}
}

func summarize(values []int64) latencySummary {
	if len(values) == 0 {
		return latencySummary{}
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum float64
	for _, value := range sorted {
		sum += float64(value)
	}
	return latencySummary{
		MeanUS: sum / float64(len(sorted)) / 1000,
		P50US:  float64(percentile(sorted, 0.50)) / 1000,
		P95US:  float64(percentile(sorted, 0.95)) / 1000,
		P99US:  float64(percentile(sorted, 0.99)) / 1000,
		MaxUS:  float64(sorted[len(sorted)-1]) / 1000,
	}
}

func percentile(sorted []int64, quantile float64) int64 {
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	index = min(max(index, 0), len(sorted)-1)
	return sorted[index]
}

func writeReport(jsonPath, markdownPath string, value report) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(markdownPath, []byte(renderMarkdown(value)), 0o644)
}

func loadReport(path string) (report, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return report{}, err
	}
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		return report{}, err
	}
	return rep, nil
}

func compareReports(baseline, current report, maxRegressionPct float64) []comparisonRow {
	baselineMap := make(map[string]scenarioResult)
	for _, res := range baseline.Results {
		baselineMap[res.Scenario] = res
	}

	var rows []comparisonRow
	for _, cur := range current.Results {
		base, exists := baselineMap[cur.Scenario]
		if !exists {
			continue
		}
		thruDelta := 0.0
		if base.ThroughputOPS > 0 {
			thruDelta = ((cur.ThroughputOPS - base.ThroughputOPS) / base.ThroughputOPS) * 100.0
		}
		p99Delta := 0.0
		if base.Latency.P99US > 0 {
			p99Delta = ((cur.Latency.P99US - base.Latency.P99US) / base.Latency.P99US) * 100.0
		}
		// Regression occurs if throughput dropped significantly or p99 latency spiked significantly
		isRegression := thruDelta < -maxRegressionPct || p99Delta > maxRegressionPct
		rows = append(rows, comparisonRow{
			Scenario:           cur.Scenario,
			BaselineThroughput: base.ThroughputOPS,
			CurrentThroughput:  cur.ThroughputOPS,
			ThroughputDeltaPct: thruDelta,
			BaselineP99US:      base.Latency.P99US,
			CurrentP99US:       cur.Latency.P99US,
			P99DeltaPct:        p99Delta,
			Regression:         isRegression,
		})
	}
	return rows
}

func renderMarkdown(value report) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# Axiom performance and resilience report\n\n")
	fmt.Fprintf(&output, "Generated: `%s`  \nGo: `%s`  \nPlatform: `%s/%s`, CPUs: `%d`\n\n", value.GeneratedAt, value.GoVersion, value.GOOS, value.GOARCH, value.CPUs)
	output.WriteString("| Scenario | Ops | C | Throughput ops/s | p50 µs | p95 µs | p99 µs | Max µs | Errors | Invariant |\n")
	output.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|:---:|\n")
	for _, result := range value.Results {
		ok := "PASS"
		if !result.InvariantOK {
			ok = "FAIL"
		}
		fmt.Fprintf(&output, "| %s | %d | %d | %.0f | %.1f | %.1f | %.1f | %.1f | %d | %s |\n",
			result.Scenario, result.Operations, result.Concurrency, result.ThroughputOPS,
			result.Latency.P50US, result.Latency.P95US, result.Latency.P99US, result.Latency.MaxUS,
			result.Errors, ok)
	}
	output.WriteString("\n## Invariants\n\n")
	for _, result := range value.Results {
		if result.Expected != 0 {
			fmt.Fprintf(&output, "- `%s`: expected `%d`, actual `%d` — **%v**.\n", result.Scenario, result.Expected, result.Actual, result.InvariantOK)
		}
	}
	return output.String()
}

func renderComparisonMarkdown(rows []comparisonRow, maxRegressionPct float64) string {
	var output strings.Builder
	output.WriteString("\n## Relative Baseline Comparison\n\n")
	output.WriteString(fmt.Sprintf("Max allowed regression threshold: `%.1f%%`\n\n", maxRegressionPct))
	output.WriteString("| Scenario | Base Ops/s | Cur Ops/s | Δ Ops/s % | Base p99 µs | Cur p99 µs | Δ p99 % | Status |\n")
	output.WriteString("|---|---:|---:|---:|---:|---:|---:|:---:|\n")
	for _, r := range rows {
		status := "PASS"
		if r.Regression {
			status = "**REGRESSION**"
		}
		output.WriteString(fmt.Sprintf("| %s | %.0f | %.0f | %+.1f%% | %.1f | %.1f | %+.1f%% | %s |\n",
			r.Scenario, r.BaselineThroughput, r.CurrentThroughput, r.ThroughputDeltaPct,
			r.BaselineP99US, r.CurrentP99US, r.P99DeltaPct, status))
	}
	return output.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
