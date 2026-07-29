// Package testutil provides shared helpers for Axiom's test and benchmark
// infrastructure. It is an internal package: external consumers must not
// depend on it.
package testutil

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// LatencyCollector accumulates raw operation durations and produces a
// LatencyReport with descriptive statistics and tail-latency percentiles.
//
// Design choice: we store every sample in a []time.Duration slice and sort
// once when Report() is called.  For 1M samples this requires ~8 MB of RAM
// (1M × 8 bytes), which is acceptable for test/bench environments.
// If memory becomes a concern, an HDR histogram can be substituted — but
// sorted-slice gives exact (not interpolated) percentile values.
// ──────────────────────────────────────────────────────────────────────────────

// LatencyCollector stores individual operation latencies for later analysis.
type LatencyCollector struct {
	samples []time.Duration
	sorted  bool
}

// NewLatencyCollector pre-allocates capacity for the expected number of
// samples. Over-estimation is cheap; under-estimation causes a realloc.
func NewLatencyCollector(expectedSamples int) *LatencyCollector {
	if expectedSamples <= 0 {
		expectedSamples = 1024
	}
	return &LatencyCollector{
		samples: make([]time.Duration, 0, expectedSamples),
	}
}

// Record adds one latency observation.  Not safe for concurrent use;
// if concurrency is needed, protect externally or use per-goroutine
// collectors and merge with Merge().
func (c *LatencyCollector) Record(d time.Duration) {
	c.samples = append(c.samples, d)
	c.sorted = false
}

// Merge appends all samples from another collector.  Useful for combining
// per-goroutine collectors after a parallel benchmark.
func (c *LatencyCollector) Merge(other *LatencyCollector) {
	if other == nil {
		return
	}
	c.samples = append(c.samples, other.samples...)
	c.sorted = false
}

// Count returns the number of recorded samples.
func (c *LatencyCollector) Count() int {
	return len(c.samples)
}

// Reset clears all samples, keeping the allocated capacity.
func (c *LatencyCollector) Reset() {
	c.samples = c.samples[:0]
	c.sorted = false
}

// LatencyReport holds computed statistics for a set of latency samples.
type LatencyReport struct {
	Count    int
	Min      time.Duration
	Max      time.Duration
	Mean     time.Duration
	Median   time.Duration // p50
	P90      time.Duration
	P95      time.Duration
	P98      time.Duration
	P99      time.Duration
	StdDev   time.Duration
	CoeffVar float64       // coefficient of variation: stddev / mean
	Total    time.Duration // sum of all samples
	OpsPerSec float64
}

// Report computes and returns descriptive statistics for all recorded
// samples. The slice is sorted in place; the sort cost is included in
// this call, not in the benchmark itself.
//
// Percentile algorithm: nearest-rank method.
//   rank = ceil(p/100 * N), clamped to [1, N].
// This is the same method used by HdrHistogram and is deterministic.
func (c *LatencyCollector) Report() LatencyReport {
	n := len(c.samples)
	if n == 0 {
		return LatencyReport{}
	}

	// Sort once — O(n log n). Subsequent calls reuse the sorted order.
	if !c.sorted {
		sort.Slice(c.samples, func(i, j int) bool {
			return c.samples[i] < c.samples[j]
		})
		c.sorted = true
	}

	var sum int64
	for _, d := range c.samples {
		sum += int64(d)
	}
	mean := float64(sum) / float64(n)

	// Standard deviation (population, not sample — we have the full dataset).
	var varianceSum float64
	for _, d := range c.samples {
		diff := float64(d) - mean
		varianceSum += diff * diff
	}
	stddev := math.Sqrt(varianceSum / float64(n))

	var coeffVar float64
	if mean > 0 {
		coeffVar = stddev / mean
	}

	report := LatencyReport{
		Count:    n,
		Min:      c.samples[0],
		Max:      c.samples[n-1],
		Mean:     time.Duration(int64(mean)),
		Median:   c.percentile(50),
		P90:      c.percentile(90),
		P95:      c.percentile(95),
		P98:      c.percentile(98),
		P99:      c.percentile(99),
		StdDev:   time.Duration(int64(stddev)),
		CoeffVar: coeffVar,
		Total:    time.Duration(sum),
	}
	if report.Total > 0 {
		report.OpsPerSec = float64(n) / report.Total.Seconds()
	}
	return report
}

// percentile returns the value at the given percentile using the
// nearest-rank method. Assumes c.samples is already sorted.
func (c *LatencyCollector) percentile(p float64) time.Duration {
	n := len(c.samples)
	if n == 0 {
		return 0
	}
	// rank = ceil(p/100 * n), 1-indexed, clamped to [1, n].
	rank := int(math.Ceil(p / 100.0 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return c.samples[rank-1]
}

// String formats the report as a human-readable multi-line table.
func (r LatencyReport) String() string {
	if r.Count == 0 {
		return "LatencyReport: no samples"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  Count:     %d\n", r.Count))
	b.WriteString(fmt.Sprintf("  Total:     %s\n", r.Total))
	b.WriteString(fmt.Sprintf("  Ops/sec:   %.0f\n", r.OpsPerSec))
	b.WriteString(fmt.Sprintf("  Min:       %s\n", r.Min))
	b.WriteString(fmt.Sprintf("  Mean:      %s\n", r.Mean))
	b.WriteString(fmt.Sprintf("  Median:    %s\n", r.Median))
	b.WriteString(fmt.Sprintf("  P90:       %s\n", r.P90))
	b.WriteString(fmt.Sprintf("  P95:       %s\n", r.P95))
	b.WriteString(fmt.Sprintf("  P98:       %s\n", r.P98))
	b.WriteString(fmt.Sprintf("  P99:       %s\n", r.P99))
	b.WriteString(fmt.Sprintf("  Max:       %s\n", r.Max))
	b.WriteString(fmt.Sprintf("  StdDev:    %s\n", r.StdDev))
	b.WriteString(fmt.Sprintf("  CoeffVar:  %.4f\n", r.CoeffVar))
	return b.String()
}

// MarkdownTable formats the report as a Markdown table row for inclusion
// in benchmark reports. The scenario parameter labels the row.
func (r LatencyReport) MarkdownTable(scenario string) string {
	return fmt.Sprintf("| %s | %d | %s | %.0f | %s | %s | %s | %s | %s | %s |",
		scenario, r.Count, r.Total, r.OpsPerSec,
		r.Mean, r.Median, r.P95, r.P98, r.P99, r.Max,
	)
}

// ──────────────────────────────────────────────────────────────────────────────
// MeasurementOverhead estimates the cost of time.Now() calls on the current
// platform. This baseline should be reported alongside latency measurements
// so that readers can judge whether instrumentation skews the results.
// ──────────────────────────────────────────────────────────────────────────────

// MeasurementOverhead runs 100 000 back-to-back time.Now() calls and returns
// the average cost per call. The result is typically 15-40 ns on modern x86.
func MeasurementOverhead() time.Duration {
	const iterations = 100_000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = time.Now()
	}
	elapsed := time.Since(start)
	return elapsed / iterations
}
