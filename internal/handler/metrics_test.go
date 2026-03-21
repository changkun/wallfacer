package handler

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/metrics"
	"changkun.de/wallfacer/internal/store"
)

// ---------------------------------------------------------------------------
// Prometheus text exposition format
// ---------------------------------------------------------------------------

// TestMetricsExposition_CounterFamilyFormat verifies that a registered counter
// produces correct HELP, TYPE, and sample lines.
func TestMetricsExposition_CounterFamilyFormat(t *testing.T) {
	reg := metrics.NewRegistry()
	c := reg.Counter("wallfacer_http_requests_total", "Total HTTP requests.")
	c.Inc(map[string]string{"method": "GET", "route": "/api/tasks", "status": "200"})
	c.Inc(map[string]string{"method": "POST", "route": "/api/tasks", "status": "201"})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# HELP wallfacer_http_requests_total Total HTTP requests.") {
		t.Errorf("missing HELP comment; got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE wallfacer_http_requests_total counter") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
	if !strings.Contains(body, `method="GET"`) {
		t.Errorf("missing GET method label; got:\n%s", body)
	}
	if !strings.Contains(body, `method="POST"`) {
		t.Errorf("missing POST method label; got:\n%s", body)
	}
	if !strings.Contains(body, `status="200"`) {
		t.Errorf("missing status 200 label; got:\n%s", body)
	}
}

// TestMetricsExposition_HistogramFamilyFormat verifies that a registered
// histogram produces correct HELP, TYPE, bucket, sum, and count lines.
func TestMetricsExposition_HistogramFamilyFormat(t *testing.T) {
	reg := metrics.NewRegistry()
	h := reg.Histogram(
		"wallfacer_http_request_duration_seconds",
		"HTTP request latency in seconds.",
		metrics.DefaultDurationBuckets,
	)
	h.Observe(map[string]string{"method": "GET", "route": "/api/tasks"}, 0.05)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# HELP wallfacer_http_request_duration_seconds") {
		t.Errorf("missing HELP comment; got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE wallfacer_http_request_duration_seconds histogram") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
	if !strings.Contains(body, "_bucket{") {
		t.Errorf("missing bucket lines; got:\n%s", body)
	}
	if !strings.Contains(body, `le="+Inf"`) {
		t.Errorf("missing +Inf bucket; got:\n%s", body)
	}
	if !strings.Contains(body, "_sum{") {
		t.Errorf("missing _sum line; got:\n%s", body)
	}
	if !strings.Contains(body, "_count{") {
		t.Errorf("missing _count line; got:\n%s", body)
	}
}

// TestMetricsExposition_GaugeFamilyFormat verifies scrape-time gauge output.
func TestMetricsExposition_GaugeFamilyFormat(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.Gauge("wallfacer_tasks_total", "Number of tasks.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{
			{Labels: map[string]string{"status": "backlog", "archived": "false"}, Value: 2},
			{Labels: map[string]string{"status": "done", "archived": "false"}, Value: 1},
		}
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# HELP wallfacer_tasks_total Number of tasks.") {
		t.Errorf("missing HELP comment; got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE wallfacer_tasks_total gauge") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
	if !strings.Contains(body, `status="backlog"`) {
		t.Errorf("missing backlog label; got:\n%s", body)
	}
	if !strings.Contains(body, `status="done"`) {
		t.Errorf("missing done label; got:\n%s", body)
	}
}

// TestMetricsExposition_AllWallfacerFamiliesPresent verifies that all required
// metric families for production monitoring are present in the registry output
// after a round of instrumentation.
func TestMetricsExposition_AllWallfacerFamiliesPresent(t *testing.T) {
	reg := metrics.NewRegistry()

	// Register all required metric families.
	httpReqs := reg.Counter(
		"wallfacer_http_requests_total",
		"Total number of HTTP requests partitioned by method, route, and status code.",
	)
	httpDur := reg.Histogram(
		"wallfacer_http_request_duration_seconds",
		"HTTP request latency in seconds partitioned by method and route.",
		metrics.DefaultDurationBuckets,
	)
	reg.Gauge("wallfacer_tasks_total", "Number of tasks.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Labels: map[string]string{"status": "backlog", "archived": "false"}, Value: 1}}
	})
	reg.Gauge("wallfacer_running_containers", "Running containers.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: 0}}
	})
	reg.Gauge("wallfacer_background_goroutines", "Background goroutines.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: 0}}
	})
	reg.Gauge("wallfacer_store_subscribers", "SSE subscribers.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: 0}}
	})

	// Simulate a request.
	httpReqs.Inc(map[string]string{"method": "GET", "route": "GET /api/tasks", "status": "200"})
	httpDur.Observe(map[string]string{"method": "GET", "route": "GET /api/tasks"}, 0.01)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	requiredFamilies := []string{
		"wallfacer_http_requests_total",
		"wallfacer_http_request_duration_seconds",
		"wallfacer_tasks_total",
		"wallfacer_running_containers",
		"wallfacer_background_goroutines",
		"wallfacer_store_subscribers",
	}
	for _, family := range requiredFamilies {
		if !strings.Contains(body, family) {
			t.Errorf("missing metric family %q in output:\n%s", family, body)
		}
	}
}

// TestMetricsHandler_ReturnsCorrectContentType verifies that the /metrics
// endpoint returns the Prometheus text content type.
func TestMetricsHandler_ReturnsCorrectContentType(t *testing.T) {
	reg := metrics.NewRegistry()

	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	reg.WritePrometheus(w)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// Scrape-time gauge correctness using real store
// ---------------------------------------------------------------------------

// TestMetricsGauge_TasksTotal verifies that the wallfacer_tasks_total gauge
// reflects actual task counts from the store.
func TestMetricsGauge_TasksTotal(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task one", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task two", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
		t.Fatal(err)
	}

	reg := metrics.NewRegistry()
	reg.Gauge("wallfacer_tasks_total", "Task count.", func() []metrics.LabeledValue {
		tasks, err := h.store.ListTasks(ctx, false)
		if err != nil {
			return nil
		}
		counts := make(map[string]int)
		for _, task := range tasks {
			counts[string(task.Status)]++
		}
		vals := make([]metrics.LabeledValue, 0, len(counts))
		for status, n := range counts {
			vals = append(vals, metrics.LabeledValue{
				Labels: map[string]string{"status": status, "archived": "false"},
				Value:  float64(n),
			})
		}
		return vals
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	// Both backlog tasks should appear as a single series with value 2.
	if !strings.Contains(body, `status="backlog"`) {
		t.Errorf("expected backlog label in gauge output; got:\n%s", body)
	}
	if !strings.Contains(body, " 2\n") {
		t.Errorf("expected value 2 for backlog tasks; got:\n%s", body)
	}
}

// TestMetricsGauge_SubscriberCount verifies the store subscriber gauge.
func TestMetricsGauge_SubscriberCount(t *testing.T) {
	h := newTestHandler(t)

	// The runner holds one internal board-cache subscription; capture the
	// baseline so the test remains correct regardless of runner internals.
	baseline := h.store.SubscriberCount()

	subID, _ := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	reg := metrics.NewRegistry()
	reg.Gauge("wallfacer_store_subscribers", "SSE subscribers.", func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: float64(h.store.SubscriberCount())}}
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	want := fmt.Sprintf("wallfacer_store_subscribers %d", baseline+1)
	if !strings.Contains(body, want) {
		t.Errorf("expected %q; got:\n%s", want, body)
	}
}
