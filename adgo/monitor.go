package adgo

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type FleetMetricsSnapshot struct {
	GeneratedAt    time.Time                    `json:"generatedAt"`
	Executions     int                          `json:"executions"`
	ByStatus       map[ExecutionStatus]int      `json:"byStatus"`
	ActiveTasks    int                          `json:"activeTasks"`
	WaitingNodes   int                          `json:"waitingNodes"`
	Budget         map[string]float64           `json:"budget"`
	RuntimeMetrics map[string]float64           `json:"runtimeMetrics"`
}

// CollectFleetMetrics aggregates only committed execution snapshots. It does not
// depend on process-local counters, so a monitoring process may run separately
// from coordinators/workers as long as it can read the same Store.
func CollectFleetMetrics(ctx context.Context, store Store) (FleetMetricsSnapshot, error) {
	catalog, ok := store.(ExecutionCatalog)
	if !ok {
		return FleetMetricsSnapshot{}, fmt.Errorf("adgo: fleet metrics require ExecutionCatalog")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return FleetMetricsSnapshot{}, err
	}
	out := FleetMetricsSnapshot{
		GeneratedAt:    time.Now().UTC(),
		ByStatus:       map[ExecutionStatus]int{},
		Budget:         map[string]float64{},
		RuntimeMetrics: map[string]float64{},
	}
	for _, id := range ids {
		execution, err := store.Load(ctx, id)
		if err != nil {
			return out, err
		}
		out.Executions++
		out.ByStatus[execution.Status]++
		out.ActiveTasks += len(execution.ActiveTasks)
		out.WaitingNodes += len(execution.WaitingFor)
		accumulateNumericStruct(out.Budget, "", execution.BudgetUsage)
		accumulateNumericStruct(out.RuntimeMetrics, "", execution.Metrics)
	}
	return out, nil
}

func accumulateNumericStruct(target map[string]float64, prefix string, value any) {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	durationType := reflect.TypeOf(time.Duration(0))
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		if !field.CanInterface() {
			continue
		}
		name := metricName(fieldType.Name)
		if prefix != "" {
			name = prefix + "_" + name
		}
		if field.Type() == durationType {
			target[name+"_seconds"] += field.Interface().(time.Duration).Seconds()
			continue
		}
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			target[name] += float64(field.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			target[name] += float64(field.Uint())
		case reflect.Float32, reflect.Float64:
			target[name] += field.Float()
		}
	}
}

func metricName(input string) string {
	var builder strings.Builder
	for index, r := range input {
		if unicode.IsUpper(r) {
			if index > 0 {
				builder.WriteByte('_')
			}
			builder.WriteRune(unicode.ToLower(r))
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

// PrometheusHandler exposes a dependency-free Prometheus text-format endpoint.
// It intentionally contains no authentication; mount it behind the same
// authenticated/internal observability boundary used by the application.
func PrometheusHandler(store Store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot, err := CollectFleetMetrics(request.Context(), store)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if request.Method == http.MethodHead {
			writer.WriteHeader(http.StatusOK)
			return
		}
		_, _ = fmt.Fprintf(writer, "# HELP adgo_executions Durable executions by status.\n# TYPE adgo_executions gauge\n")
		statuses := make([]string, 0, len(snapshot.ByStatus))
		for status := range snapshot.ByStatus {
			statuses = append(statuses, string(status))
		}
		sort.Strings(statuses)
		for _, status := range statuses {
			_, _ = fmt.Fprintf(writer, "adgo_executions{status=%s} %d\n", strconv.Quote(status), snapshot.ByStatus[ExecutionStatus(status)])
		}
		_, _ = fmt.Fprintf(writer, "# TYPE adgo_executions_total gauge\nadgo_executions_total %d\n", snapshot.Executions)
		_, _ = fmt.Fprintf(writer, "# TYPE adgo_active_tasks gauge\nadgo_active_tasks %d\n", snapshot.ActiveTasks)
		_, _ = fmt.Fprintf(writer, "# TYPE adgo_waiting_nodes gauge\nadgo_waiting_nodes %d\n", snapshot.WaitingNodes)
		writePrometheusMap(writer, "adgo_budget_", snapshot.Budget)
		writePrometheusMap(writer, "adgo_runtime_", snapshot.RuntimeMetrics)
	})
}

func writePrometheusMap(writer http.ResponseWriter, prefix string, values map[string]float64) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = fmt.Fprintf(writer, "# TYPE %s%s gauge\n%s%s %s\n", prefix, key, prefix, key, strconv.FormatFloat(values[key], 'g', -1, 64))
	}
}
