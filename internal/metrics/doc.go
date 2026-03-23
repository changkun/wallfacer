// Package metrics provides a lightweight Prometheus-compatible metrics registry
// without external dependencies.
//
// It supports labeled counters, histograms with configurable buckets, and
// scrape-time gauge callbacks. The registry is thread-safe and can write its
// contents in Prometheus text exposition format. This avoids pulling in the
// full Prometheus client library while still enabling standard metrics scraping.
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/pkg/sortedkeys] for deterministic
// label ordering in output. Consumed by [cli] (creates the registry), [handler]
// (instruments HTTP requests and registers gauges), and [runner] (records task
// execution metrics). When adding new metrics, define them where they are recorded
// and pass the registry through.
//
// # Usage
//
//	reg := metrics.NewRegistry()
//	counter := reg.Counter("http_requests_total", "Total HTTP requests")
//	counter.Inc(map[string]string{"method": "GET", "path": "/api/tasks"})
//	hist := reg.Histogram("request_duration_seconds", "Request latency", metrics.DefaultDurationBuckets)
//	hist.Observe(nil, elapsed.Seconds())
//	reg.WritePrometheus(w)
package metrics
