package metrics

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// canonicalLabelKey
// ---------------------------------------------------------------------------

func TestCanonicalLabelKey_Empty(t *testing.T) {
	if got := canonicalLabelKey(nil); got != "" {
		t.Errorf("expected empty string for nil labels, got %q", got)
	}
	if got := canonicalLabelKey(map[string]string{}); got != "" {
		t.Errorf("expected empty string for empty labels, got %q", got)
	}
}

func TestCanonicalLabelKey_Deterministic(t *testing.T) {
	labels := map[string]string{"method": "GET", "route": "/api/tasks", "status": "200"}
	k1 := canonicalLabelKey(labels)
	k2 := canonicalLabelKey(labels)
	if k1 != k2 {
		t.Errorf("canonicalLabelKey not deterministic: %q vs %q", k1, k2)
	}
}

func TestCanonicalLabelKey_OrderIndependent(t *testing.T) {
	a := canonicalLabelKey(map[string]string{"a": "1", "b": "2"})
	b := canonicalLabelKey(map[string]string{"b": "2", "a": "1"})
	if a != b {
		t.Errorf("canonicalLabelKey should be order-independent: %q vs %q", a, b)
	}
}

func TestCanonicalLabelKey_DistinguishesLabelSets(t *testing.T) {
	k1 := canonicalLabelKey(map[string]string{"method": "GET", "status": "200"})
	k2 := canonicalLabelKey(map[string]string{"method": "POST", "status": "200"})
	if k1 == k2 {
		t.Error("different label sets must produce different keys")
	}
}

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

func TestCounter_IncSingleLabel(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("test_counter", "A test counter.")
	c.Inc(map[string]string{"code": "200"})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, `code="200"`) {
		t.Errorf("expected label in output; got:\n%s", body)
	}
	if !strings.Contains(body, " 1\n") {
		t.Errorf("expected counter value 1 in output; got:\n%s", body)
	}
}

func TestCounter_AddAccumulates(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("acc_counter", "Accumulating counter.")
	labels := map[string]string{"x": "y"}
	c.Add(labels, 3)
	c.Add(labels, 7)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, " 10\n") {
		t.Errorf("expected accumulated value 10; got:\n%s", body)
	}
}

func TestCounter_MultipleLabelSets(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("multi_counter", "Multi label counter.")
	c.Inc(map[string]string{"status": "200"})
	c.Inc(map[string]string{"status": "404"})
	c.Inc(map[string]string{"status": "200"})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	// Two distinct label sets must appear.
	if !strings.Contains(body, `status="200"`) || !strings.Contains(body, `status="404"`) {
		t.Errorf("expected both label sets in output; got:\n%s", body)
	}
}

func TestCounter_HelpAndTypeComments(t *testing.T) {
	reg := NewRegistry()
	reg.Counter("wallfacer_http_requests_total", "Total HTTP requests.")
	// No observations; header lines should still appear.

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# HELP wallfacer_http_requests_total Total HTTP requests.") {
		t.Errorf("missing HELP comment; got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE wallfacer_http_requests_total counter") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
}

func TestCounter_RegistryDeduplicates(t *testing.T) {
	reg := NewRegistry()
	c1 := reg.Counter("dup", "help")
	c2 := reg.Counter("dup", "help")
	if c1 != c2 {
		t.Error("Counter() should return the same pointer for the same name")
	}
}

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

func TestHistogram_ObserveSingleValue(t *testing.T) {
	reg := NewRegistry()
	h := reg.Histogram("req_duration", "Duration.", []float64{0.1, 0.5, 1.0})
	h.Observe(map[string]string{"method": "GET"}, 0.05)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# TYPE req_duration histogram") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
	if !strings.Contains(body, `le="0.1"`) {
		t.Errorf("missing le=0.1 bucket; got:\n%s", body)
	}
	if !strings.Contains(body, `le="+Inf"`) {
		t.Errorf("missing +Inf bucket; got:\n%s", body)
	}
	if !strings.Contains(body, "req_duration_sum") {
		t.Errorf("missing _sum line; got:\n%s", body)
	}
	if !strings.Contains(body, "req_duration_count") {
		t.Errorf("missing _count line; got:\n%s", body)
	}
}

func TestHistogram_CumulativeBuckets(t *testing.T) {
	// An observation of 0.05 should fall in the le=0.1 bucket (and higher)
	// but NOT in le=0.01.
	reg := NewRegistry()
	h := reg.Histogram("latency", "Latency.", []float64{0.01, 0.1, 1.0})
	labels := map[string]string{"route": "/api"}
	h.Observe(labels, 0.05)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	lines := strings.Split(body, "\n")
	bucketVal := func(le string) string {
		prefix := fmt.Sprintf(`latency_bucket{le="%s",route="/api"} `, le)
		for _, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return strings.TrimPrefix(l, prefix)
			}
		}
		return ""
	}

	if bucketVal("0.01") != "0" {
		t.Errorf("expected le=0.01 bucket to be 0 for observation 0.05, body:\n%s", body)
	}
	if bucketVal("0.1") != "1" {
		t.Errorf("expected le=0.1 bucket to be 1 for observation 0.05, body:\n%s", body)
	}
	if bucketVal("+Inf") != "1" {
		t.Errorf("expected +Inf bucket to be 1, body:\n%s", body)
	}
}

func TestHistogram_SumAndCount(t *testing.T) {
	reg := NewRegistry()
	h := reg.Histogram("h", "help", DefaultDurationBuckets)
	labels := map[string]string{"x": "1"}
	h.Observe(labels, 0.1)
	h.Observe(labels, 0.2)
	h.Observe(labels, 0.3)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, `h_count{x="1"} 3`) {
		t.Errorf("expected count=3; got:\n%s", body)
	}
	// sum should be ~0.6
	if !strings.Contains(body, `h_sum{x="1"} 0.6`) {
		t.Errorf("expected sum≈0.6; got:\n%s", body)
	}
}

func TestHistogram_RegistryDeduplicates(t *testing.T) {
	reg := NewRegistry()
	h1 := reg.Histogram("dup_hist", "help", DefaultDurationBuckets)
	h2 := reg.Histogram("dup_hist", "help", DefaultDurationBuckets)
	if h1 != h2 {
		t.Error("Histogram() should return the same pointer for the same name")
	}
}

func TestHistogram_BucketsAreSorted(t *testing.T) {
	reg := NewRegistry()
	// Pass unsorted buckets to verify they are sorted on creation.
	h := reg.Histogram("sort_hist", "help", []float64{1.0, 0.1, 0.5})
	h.Observe(map[string]string{}, 0.3)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	// le=0.1 should come before le=0.5 in output.
	idx01 := strings.Index(body, `le="0.1"`)
	idx05 := strings.Index(body, `le="0.5"`)
	idx10 := strings.Index(body, `le="1"`)
	if idx01 < 0 || idx05 < 0 || idx10 < 0 {
		t.Fatalf("expected all three bucket lines; got:\n%s", body)
	}
	if idx01 >= idx05 || idx05 >= idx10 {
		t.Errorf("bucket lines out of order; got:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Gauge
// ---------------------------------------------------------------------------

func TestGauge_CallsFnOnScrape(t *testing.T) {
	reg := NewRegistry()
	calls := 0
	reg.Gauge("g", "help", func() []LabeledValue {
		calls++
		return []LabeledValue{{Value: float64(calls)}}
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	if calls != 1 {
		t.Errorf("expected fn called once, got %d", calls)
	}
	reg.WritePrometheus(&sb)
	if calls != 2 {
		t.Errorf("expected fn called twice on second scrape, got %d", calls)
	}
}

func TestGauge_HelpAndTypeComments(t *testing.T) {
	reg := NewRegistry()
	reg.Gauge("tasks_total", "Number of tasks.", func() []LabeledValue {
		return []LabeledValue{{Labels: map[string]string{"status": "backlog"}, Value: 5}}
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, "# HELP tasks_total Number of tasks.") {
		t.Errorf("missing HELP comment; got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE tasks_total gauge") {
		t.Errorf("missing TYPE comment; got:\n%s", body)
	}
}

func TestGauge_EmptySliceSkipped(t *testing.T) {
	reg := NewRegistry()
	reg.Gauge("empty_gauge", "help", func() []LabeledValue { return nil })

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if strings.Contains(body, "empty_gauge") {
		t.Errorf("empty gauge should produce no output; got:\n%s", body)
	}
}

func TestGauge_WithLabels(t *testing.T) {
	reg := NewRegistry()
	reg.Gauge("wallfacer_tasks_total", "Task count.", func() []LabeledValue {
		return []LabeledValue{
			{Labels: map[string]string{"status": "backlog", "archived": "false"}, Value: 3},
			{Labels: map[string]string{"status": "done", "archived": "true"}, Value: 7},
		}
	})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	if !strings.Contains(body, `status="backlog"`) {
		t.Errorf("missing backlog label; got:\n%s", body)
	}
	if !strings.Contains(body, `archived="true"`) {
		t.Errorf("missing archived label; got:\n%s", body)
	}
	if !strings.Contains(body, " 7\n") {
		t.Errorf("expected value 7; got:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// WritePrometheus ordering and format
// ---------------------------------------------------------------------------

func TestWritePrometheus_CountersBeforeHistogramsBeforeGauges(t *testing.T) {
	reg := NewRegistry()
	reg.Gauge("zz_gauge", "g", func() []LabeledValue {
		return []LabeledValue{{Value: 1}}
	})
	h := reg.Histogram("mm_hist", "h", DefaultDurationBuckets)
	h.Observe(nil, 0.1)
	c := reg.Counter("aa_counter", "c")
	c.Inc(nil)

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	idxCounter := strings.Index(body, "aa_counter")
	idxHist := strings.Index(body, "mm_hist")
	idxGauge := strings.Index(body, "zz_gauge")

	if idxCounter < 0 || idxHist < 0 || idxGauge < 0 {
		t.Fatalf("missing metric families; got:\n%s", body)
	}
	if idxCounter >= idxHist || idxHist >= idxGauge {
		t.Errorf("expected counters < histograms < gauges; indices: counter=%d hist=%d gauge=%d\nbody:\n%s",
			idxCounter, idxHist, idxGauge, body)
	}
}

func TestWritePrometheus_MetricNameFormat(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("wallfacer_http_requests_total", "help")
	c.Inc(map[string]string{"method": "GET", "route": "/api/tasks", "status": "200"})

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	// Labels must be sorted alphabetically (method < route < status).
	want := `wallfacer_http_requests_total{method="GET",route="/api/tasks",status="200"} 1`
	if !strings.Contains(body, want) {
		t.Errorf("expected line %q in output; got:\n%s", want, body)
	}
}

// ---------------------------------------------------------------------------
// escapeLabel
// ---------------------------------------------------------------------------

func TestEscapeLabel_Backslash(t *testing.T) {
	if got := escapeLabel(`a\b`); got != `a\\b` {
		t.Errorf("expected backslash escape, got %q", got)
	}
}

func TestEscapeLabel_Quote(t *testing.T) {
	if got := escapeLabel(`say "hi"`); got != `say \"hi\"` {
		t.Errorf("expected quote escape, got %q", got)
	}
}

func TestEscapeLabel_Newline(t *testing.T) {
	if got := escapeLabel("line1\nline2"); got != `line1\nline2` {
		t.Errorf("expected newline escape, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatMetricValue
// ---------------------------------------------------------------------------

func TestFormatMetricValue_PosInf(t *testing.T) {
	if got := formatMetricValue(math.Inf(1)); got != "+Inf" {
		t.Errorf("expected +Inf, got %q", got)
	}
}

func TestFormatMetricValue_NegInf(t *testing.T) {
	if got := formatMetricValue(math.Inf(-1)); got != "-Inf" {
		t.Errorf("expected -Inf, got %q", got)
	}
}

func TestFormatMetricValue_NaN(t *testing.T) {
	if got := formatMetricValue(math.NaN()); got != "NaN" {
		t.Errorf("expected NaN, got %q", got)
	}
}

func TestFormatMetricValue_Integer(t *testing.T) {
	if got := formatMetricValue(42); got != "42" {
		t.Errorf("expected 42, got %q", got)
	}
}

func TestFormatMetricValue_Float(t *testing.T) {
	if got := formatMetricValue(0.005); got != "0.005" {
		t.Errorf("expected 0.005, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Concurrency smoke test
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentIncrement(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("concurrent", "help")
	labels := map[string]string{"k": "v"}

	const goroutines = 100
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			c.Inc(labels)
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	want := fmt.Sprintf(`concurrent{k="v"} %d`, goroutines)
	if !strings.Contains(body, want) {
		t.Errorf("expected %q; got:\n%s", want, body)
	}
}

func TestRegistry_ConcurrentObserve(t *testing.T) {
	reg := NewRegistry()
	h := reg.Histogram("concurrent_hist", "help", DefaultDurationBuckets)
	labels := map[string]string{"k": "v"}

	const goroutines = 50
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			h.Observe(labels, 0.01)
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	want := fmt.Sprintf(`concurrent_hist_count{k="v"} %d`, goroutines)
	if !strings.Contains(body, want) {
		t.Errorf("expected %q; got:\n%s", want, body)
	}
}
